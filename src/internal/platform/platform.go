package platform

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

func ID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

func Now() int64 { return time.Now().UnixMicro() }

func Hash(value []byte) string {
	hash := sha256.Sum256(value)
	return hex.EncodeToString(hash[:])
}

func JSONHash(value any) string {
	body, _ := json.Marshal(value)
	return Hash(body)
}

func Token(bytes int) string {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value)
}

func Map(value any) map[string]any {
	body, _ := json.Marshal(value)
	var result map[string]any
	_ = json.Unmarshal(body, &result)
	return result
}
