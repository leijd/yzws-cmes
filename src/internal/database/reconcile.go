package database

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	createTablePattern    = regexp.MustCompile(`(?is)^CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-zA-Z_][a-zA-Z0-9_]*)\s*\((.*)\)$`)
	createIndexPattern    = regexp.MustCompile(`(?is)^CREATE\s+(?:UNIQUE\s+)?INDEX\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+ON\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	createFunctionPattern = regexp.MustCompile(`(?is)^CREATE\s+FUNCTION\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\(`)
	createTriggerPattern  = regexp.MustCompile(`(?is)^CREATE\s+TRIGGER\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+.*\sON\s+([a-zA-Z_][a-zA-Z0-9_]*)\s`)
	alterAddColumnPattern = regexp.MustCompile(`(?is)^ALTER\s+TABLE\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+ADD\s+COLUMN\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-zA-Z_][a-zA-Z0-9_]*)\s`)
	constraintCastPattern = regexp.MustCompile(`::[a-zA-Z_][a-zA-Z0-9_]*`)
	anyArrayPattern       = regexp.MustCompile(`(?is)([a-zA-Z_][a-zA-Z0-9_]*)\s*=\s*ANY\s*\(\s*ARRAY\s*\[(.*?)\]\s*\)`)
)

func Reconcile(ctx context.Context, db *pgxpool.Pool) error {
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tables, indexes, triggers := 0, 0, 0
	for _, entry := range entries {
		body, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		for _, statement := range splitSQLStatements(string(body)) {
			statement = strings.TrimSpace(statement)
			switch {
			case alterAddColumnPattern.MatchString(statement):
				matches := alterAddColumnPattern.FindStringSubmatch(statement)
				columns, err := existingColumns(ctx, tx, matches[1])
				if err != nil {
					return err
				}
				if !columns[matches[2]] {
					log.Printf("schema repair: add column %s.%s", matches[1], matches[2])
					if _, err := tx.Exec(ctx, statement); err != nil {
						return err
					}
				}
			case createTablePattern.MatchString(statement):
				if err := reconcileTable(ctx, tx, statement); err != nil {
					return fmt.Errorf("reconcile %s: %w", entry.Name(), err)
				}
				tables++
			case createIndexPattern.MatchString(statement):
				matches := createIndexPattern.FindStringSubmatch(statement)
				exists, err := objectExists(ctx, tx, "SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE schemaname='public' AND indexname=$1 AND tablename=$2)", matches[1], matches[2])
				if err != nil {
					return err
				}
				if !exists {
					log.Printf("schema repair: create index %s", matches[1])
					if _, err := tx.Exec(ctx, statement); err != nil {
						return err
					}
				}
				indexes++
			case createFunctionPattern.MatchString(statement):
				name := createFunctionPattern.FindStringSubmatch(statement)[1]
				exists, err := objectExists(ctx, tx, "SELECT EXISTS(SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' AND p.proname=$1)", name)
				if err != nil {
					return err
				}
				if !exists {
					log.Printf("schema repair: create function %s", name)
					if _, err := tx.Exec(ctx, statement); err != nil {
						return err
					}
				}
			case createTriggerPattern.MatchString(statement):
				matches := createTriggerPattern.FindStringSubmatch(statement)
				exists, err := objectExists(ctx, tx, `SELECT EXISTS(SELECT 1 FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND t.tgname=$1 AND c.relname=$2 AND NOT t.tgisinternal)`, matches[1], matches[2])
				if err != nil {
					return err
				}
				if !exists {
					log.Printf("schema repair: create trigger %s on %s", matches[1], matches[2])
					if _, err := tx.Exec(ctx, statement); err != nil {
						return err
					}
				}
				triggers++
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	log.Printf("schema check complete: %d tables, %d indexes, %d triggers verified", tables, indexes, triggers)
	return nil
}

func reconcileTable(ctx context.Context, tx pgx.Tx, statement string) error {
	matches := createTablePattern.FindStringSubmatch(statement)
	table, body := matches[1], matches[2]
	exists, err := objectExists(ctx, tx, "SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=$1)", table)
	if err != nil {
		return err
	}
	if !exists {
		log.Printf("schema repair: create table %s", table)
		_, err := tx.Exec(ctx, statement)
		return err
	}
	columns, err := existingColumns(ctx, tx, table)
	if err != nil {
		return err
	}
	constraints, err := existingConstraints(ctx, tx, table)
	if err != nil {
		return err
	}
	for _, definition := range splitTopLevel(body) {
		definition = strings.TrimSpace(definition)
		if definition == "" {
			continue
		}
		if isTableConstraint(definition) {
			if !constraints[normalizeConstraint(definition)] {
				log.Printf("schema repair: add constraint to %s", table)
				if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLE %s ADD %s", table, definition)); err != nil {
					return err
				}
				constraints[normalizeConstraint(definition)] = true
			}
			continue
		}
		fields := strings.Fields(definition)
		if len(fields) < 2 {
			return fmt.Errorf("invalid column definition on %s: %s", table, definition)
		}
		column := strings.Trim(fields[0], `"`)
		if !columns[column] {
			log.Printf("schema repair: add column %s.%s", table, column)
			if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", table, definition)); err != nil {
				return err
			}
			columns[column] = true
		}
		for _, constraint := range inlineConstraints(column, definition) {
			if !constraints[normalizeConstraint(constraint)] {
				log.Printf("schema repair: add inline constraint to %s.%s", table, column)
				if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLE %s ADD %s", table, constraint)); err != nil {
					return err
				}
				constraints[normalizeConstraint(constraint)] = true
			}
		}
	}
	return nil
}

func existingColumns(ctx context.Context, tx pgx.Tx, table string) (map[string]bool, error) {
	rows, err := tx.Query(ctx, "SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name=$1", table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		result[name] = true
	}
	return result, rows.Err()
}

func existingConstraints(ctx context.Context, tx pgx.Tx, table string) (map[string]bool, error) {
	rows, err := tx.Query(ctx, "SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=$1::regclass", table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]bool{}
	for rows.Next() {
		var definition string
		if err := rows.Scan(&definition); err != nil {
			return nil, err
		}
		result[normalizeConstraint(definition)] = true
	}
	return result, rows.Err()
}

func inlineConstraints(column, definition string) []string {
	upper := strings.ToUpper(definition)
	result := []string{}
	if strings.Contains(upper, "PRIMARY KEY") {
		result = append(result, fmt.Sprintf("PRIMARY KEY(%s)", column))
	}
	if hasStandaloneKeyword(upper, "UNIQUE") {
		result = append(result, fmt.Sprintf("UNIQUE(%s)", column))
	}
	if index := strings.Index(upper, "REFERENCES "); index >= 0 {
		result = append(result, fmt.Sprintf("FOREIGN KEY(%s) %s", column, definition[index:]))
	}
	if index := strings.Index(upper, "CHECK"); index >= 0 {
		result = append(result, definition[index:])
	}
	return result
}

func hasStandaloneKeyword(definition, keyword string) bool {
	for _, field := range strings.Fields(definition) {
		if strings.Trim(field, "(),") == keyword {
			return true
		}
	}
	return false
}

func isTableConstraint(definition string) bool {
	upper := strings.ToUpper(definition)
	return strings.HasPrefix(upper, "PRIMARY KEY") || strings.HasPrefix(upper, "UNIQUE") || strings.HasPrefix(upper, "FOREIGN KEY") || strings.HasPrefix(upper, "CHECK") || strings.HasPrefix(upper, "CONSTRAINT")
}

func normalizeConstraint(definition string) string {
	normalized := anyArrayPattern.ReplaceAllString(definition, "${1} IN (${2})")
	normalized = constraintCastPattern.ReplaceAllString(strings.ToLower(normalized), "")
	return strings.NewReplacer(" ", "", "\n", "", "\r", "", "\t", "", "(", "", ")", "", `"`, "", "'", "").Replace(normalized)
}

func objectExists(ctx context.Context, tx pgx.Tx, query string, args ...any) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, query, args...).Scan(&exists)
	return exists, err
}

func splitSQLStatements(input string) []string {
	statements := []string{}
	start := 0
	for i := 0; i < len(input); i++ {
		switch input[i] {
		case '\'':
			i = skipQuoted(input, i, '\'')
		case '"':
			i = skipQuoted(input, i, '"')
		case '$':
			if tag := dollarTag(input[i:]); tag != "" {
				closing := strings.Index(input[i+len(tag):], tag)
				if closing >= 0 {
					i += len(tag) + closing + len(tag) - 1
				}
			}
		case ';':
			if statement := strings.TrimSpace(input[start:i]); statement != "" {
				statements = append(statements, statement)
			}
			start = i + 1
		}
	}
	if statement := strings.TrimSpace(input[start:]); statement != "" {
		statements = append(statements, statement)
	}
	return statements
}

func skipQuoted(input string, start int, quote byte) int {
	for i := start + 1; i < len(input); i++ {
		if input[i] == quote {
			if i+1 < len(input) && input[i+1] == quote {
				i++
				continue
			}
			return i
		}
	}
	return len(input) - 1
}

func dollarTag(input string) string {
	if len(input) < 2 || input[0] != '$' {
		return ""
	}
	for i := 1; i < len(input); i++ {
		if input[i] == '$' {
			return input[:i+1]
		}
		if input[i] != '_' && (input[i] < 'a' || input[i] > 'z') && (input[i] < 'A' || input[i] > 'Z') && (input[i] < '0' || input[i] > '9') {
			return ""
		}
	}
	return ""
}

func splitTopLevel(input string) []string {
	result, start, depth := []string{}, 0, 0
	for i := 0; i < len(input); i++ {
		switch input[i] {
		case '\'':
			i = skipQuoted(input, i, '\'')
		case '"':
			i = skipQuoted(input, i, '"')
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				result = append(result, input[start:i])
				start = i + 1
			}
		}
	}
	result = append(result, input[start:])
	return result
}
