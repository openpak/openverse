// Package linkcodes is the web half of linking a console or old browser to an account. A device
// asks Juxtaposition's Lite service for a short code and shows it; the person types it on the
// Linked devices page and confirms. The code lives in Postgres (table ui_kv) under the key shape the Lite service
// writes (apps/juxtaposition-ui/src/lite/linkCodes.ts), so this package and that one share it:
//
//	verse-lite:code:<CODE>  ->  {"deviceClass","secretHash","ip","createdAt","accountId"}   (10 minutes)
//
// The device polls with a secret only it holds; the code alone grants nothing. This package never
// reads the secret hash, it only copies it back when it sets accountId.
package linkcodes

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

const (
	alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789" // no 0 O 1 I L
	length   = 6
	ttl      = 600
	// MaxActive is how many links one account may hold (the device-links API enforces it too).
	MaxActive = 12
)

// KV is the two key-value calls used.
type KV interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key, value string, ex int) error
}

// Entry is what Lite stored for a code. Unknown fields are kept when it is rewritten.
type Entry struct {
	DeviceClass string  `json:"deviceClass"`
	SecretHash  string  `json:"secretHash"`
	IP          string  `json:"ip"`
	CreatedAt   int64   `json:"createdAt"` // milliseconds
	AccountID   *string `json:"accountId"`
}

func key(code string) string { return "verse-lite:code:" + code }

// Normalize turns "abc 23x" into "ABC23X"; ok is false when it cannot be a code.
func Normalize(in string) (string, bool) {
	code := strings.ToUpper(in)
	code = strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(code)
	if len(code) != length {
		return "", false
	}
	for _, r := range code {
		if !strings.ContainsRune(alphabet, r) {
			return "", false
		}
	}
	return code, true
}

// Peek reads a code. nil means unknown or expired.
func Peek(ctx context.Context, kv KV, code string) (*Entry, error) {
	raw, ok, err := kv.Get(ctx, key(code))
	if err != nil || !ok {
		return nil, err
	}
	var e Entry
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		return nil, nil
	}
	return &e, nil
}

// Approve ties a pending code to an account. False when the code is gone, expired or already approved.
func Approve(ctx context.Context, kv KV, code, account string, now time.Time) (bool, error) {
	e, err := Peek(ctx, kv, code)
	if err != nil || e == nil || e.AccountID != nil {
		return false, err
	}
	e.AccountID = &account
	left := ttl - int((now.UnixMilli()-e.CreatedAt)/1000)
	if left < 30 {
		left = 30
	}
	b, _ := json.Marshal(e)
	return true, kv.Set(ctx, key(code), string(b), left)
}
