package sms

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

/**
 * 腾讯云短信（sms.tencentcloudapi.com SendSms，v2021-01-11，GET + HMAC-SHA1 签名）。
 * 环境变量：
 *   TENCENT_SECRET_ID / TENCENT_SECRET_KEY  云 API 密钥
 *   TENCENT_SDKAppID                        短信应用 SdkAppId（如 1400006666）
 *   TENCENT_SMS_SIGN_NAME                   短信签名内容（如“腾讯云”，须已审核通过）
 *   TENCENT_SMS_TEMPLATE_ID                 验证码模板 ID（模板参数仅验证码本身，如“您的验证码为{1}”）
 *   TENCENT_SMS_REGION                      可选，默认 ap-guangzhou
 * 配齐即自动启用：验证码不再走演示模式，直接真实下发。
 */
const tencentURLScheme = "tencentcloud"

type TencentEnv struct {
	SecretID  string
	SecretKey string
	SDKAppID  string
	SignName  string
	TemplateID string
	Region    string
}

func TencentFromEnv() *TencentEnv {
	env := getenvTrim("TENCENT_SECRET_ID", "TENCENT_SECRET_KEY", "TENCENT_SDKAppID", "TENCENT_SMS_SIGN_NAME", "TENCENT_SMS_TEMPLATE_ID")
	if env == nil {
		return nil
	}
	region := strings.TrimSpace(os.Getenv("TENCENT_SMS_REGION"))
	if region == "" {
		region = "ap-guangzhou"
	}
	return &TencentEnv{
		SecretID: env["TENCENT_SECRET_ID"], SecretKey: env["TENCENT_SECRET_KEY"],
		SDKAppID: env["TENCENT_SDKAppID"], SignName: env["TENCENT_SMS_SIGN_NAME"],
		TemplateID: env["TENCENT_SMS_TEMPLATE_ID"], Region: region,
	}
}

func getenvTrim(keys ...string) map[string]string {
	out := map[string]string{}
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			return nil
		}
		out[key] = value
	}
	return out
}

// Provider 构造内置腾讯云服务商（URL 使用保留 scheme，sendOnce 识别后走 sendTencent）。
func (e *TencentEnv) Provider() Provider {
	enabled := true
	return Provider{
		Name: "tencent-cloud", URL: tencentURLScheme, Enabled: &enabled, Weight: 100, TimeoutMS: 8000,
		Extra: map[string]any{
			"secret_id": e.SecretID, "secret_key": e.SecretKey, "sdk_app_id": e.SDKAppID,
			"sign_name": e.SignName, "template_id": e.TemplateID, "region": e.Region,
		},
	}
}

func tencentExtra(p Provider, key string) string {
	if v, ok := p.Extra[key].(string); ok {
		return v
	}
	return ""
}

func sendTencent(p Provider, phone, code string) error {
	secretKey := tencentExtra(p, "secret_key")
	nonce, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return err
	}
	params := map[string]string{
		"Nonce":              strconv.FormatInt(nonce.Int64(), 10),
		"Timestamp":          strconv.FormatInt(time.Now().Unix(), 10),
		"Region":             tencentExtra(p, "region"),
		"SecretId":           tencentExtra(p, "secret_id"),
		"Version":            "2021-01-11",
		"Action":             "SendSms",
		"SmsSdkAppId":        tencentExtra(p, "sdk_app_id"),
		"SignName":           tencentExtra(p, "sign_name"),
		"TemplateId":         tencentExtra(p, "template_id"),
		"TemplateParamSet.0": code,
		"PhoneNumberSet.0":   "+86" + phone,
		"SessionContext":     "clothp-verify",
	}
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var canonical bytes.Buffer
	canonical.WriteString("GETsms.tencentcloudapi.com/?")
	for i, key := range keys {
		if i > 0 {
			canonical.WriteByte('&')
		}
		canonical.WriteString(key)
		canonical.WriteByte('=')
		canonical.WriteString(params[key])
	}
	mac := hmac.New(sha1.New, []byte(secretKey))
	mac.Write(canonical.Bytes())
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	var query bytes.Buffer
	keys = append(keys, "Signature")
	params["Signature"] = signature
	sort.Strings(keys)
	for i, key := range keys {
		if i > 0 {
			query.WriteByte('&')
		}
		query.WriteString(key)
		query.WriteByte('=')
		query.WriteString(url.QueryEscape(params[key]))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://sms.tencentcloudapi.com/?"+query.String(), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tencent sms http %d: %s", resp.StatusCode, truncateForLog(body))
	}
	var result struct {
		Response struct {
			SendStatusSet []struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"SendStatusSet"`
			Error *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("tencent sms bad response: %w", err)
	}
	if result.Response.Error != nil {
		return fmt.Errorf("tencent sms api error %s: %s", result.Response.Error.Code, result.Response.Error.Message)
	}
	if len(result.Response.SendStatusSet) == 0 {
		return fmt.Errorf("tencent sms empty status set")
	}
	status := result.Response.SendStatusSet[0]
	if status.Code != "Ok" {
		return fmt.Errorf("tencent sms send failed %s: %s", status.Code, status.Message)
	}
	return nil
}

func truncateForLog(body []byte) string {
	if len(body) > 256 {
		return string(body[:256])
	}
	return string(body)
}
