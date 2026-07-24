package antibot

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/TryEarful/earful/internal/clock"
)

// FormTokens issues HMAC-signed render timestamps for respondent forms.
// They give the no-JS path real bot resistance: a submission that arrives
// seconds after the form was rendered is automation, not a person reading
// questions — and unlike proof-of-work, checking a timestamp needs no
// JavaScript at all.
//
// The key is random per process. A deploy therefore invalidates forms
// that were open across it; the respondent sees a "please try again"
// re-render with their answers intact, which is acceptable at MVP scale
// and keeps key management at zero.
type FormTokens struct {
	key   []byte
	clock clock.Clock
}

func NewFormTokens(c clock.Clock) *FormTokens {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("antibot: crypto/rand failed: " + err.Error())
	}
	return &FormTokens{key: key, clock: c}
}

var (
	// ErrFormTooFast means the form came back faster than a human reads.
	ErrFormTooFast = errors.New("antibot: form submitted too fast")
	// ErrFormTokenInvalid means the token is missing, malformed, forged,
	// or older than any legitimately open form.
	ErrFormTokenInvalid = errors.New("antibot: form token invalid")
)

// maxFormAge bounds how long a rendered form stays submittable. Generous:
// someone can leave a survey open overnight and finish over breakfast.
const maxFormAge = 24 * time.Hour

// Issue returns a signed token binding "now" to scope (the survey id).
func (f *FormTokens) Issue(scope string) string {
	ts := strconv.FormatInt(f.clock.Now().UnixMilli(), 10)
	return ts + "." + f.sign(scope, ts)
}

// Check verifies the token and enforces the minimum age.
func (f *FormTokens) Check(scope, token string, minAge time.Duration) error {
	ts, sig, ok := strings.Cut(token, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(f.sign(scope, ts))) {
		return ErrFormTokenInvalid
	}
	millis, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return ErrFormTokenInvalid
	}
	age := f.clock.Now().Sub(time.UnixMilli(millis))
	if age < 0 || age > maxFormAge {
		return ErrFormTokenInvalid
	}
	if age < minAge {
		return ErrFormTooFast
	}
	return nil
}

func (f *FormTokens) sign(scope, ts string) string {
	mac := hmac.New(sha256.New, f.key)
	fmt.Fprintf(mac, "%s|%s", scope, ts)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
