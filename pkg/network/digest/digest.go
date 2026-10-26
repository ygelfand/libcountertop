package digest

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

type Credentials func() (user, password string)

func (c Credentials) Open() bool {
	if c == nil {
		return true
	}
	_, password := c()
	return password == ""
}

type Challenge struct {
	Realm string
	Nonce string
}

func New(realm string) Challenge {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return Challenge{Realm: realm, Nonce: hex.EncodeToString(b)}
}

func (c Challenge) Header() string {
	return fmt.Sprintf(`Digest realm="%s", nonce="%s"`, c.Realm, c.Nonce)
}

func (c Challenge) Allows(authorization, method string, creds Credentials) bool {
	if creds.Open() {
		return true
	}
	scheme, rest, _ := strings.Cut(strings.TrimSpace(authorization), " ")
	if !strings.EqualFold(scheme, "Digest") {
		return false
	}
	p := params(rest)
	user, password := creds()
	if p["username"] != user || p["realm"] != c.Realm || p["nonce"] != c.Nonce {
		return false
	}
	ha1 := sum(user + ":" + c.Realm + ":" + password)
	ha2 := sum(method + ":" + p["uri"])
	want := sum(ha1 + ":" + c.Nonce + ":" + ha2)
	if qop := p["qop"]; qop != "" {
		want = sum(ha1 + ":" + c.Nonce + ":" + p["nc"] + ":" + p["cnonce"] + ":" + qop + ":" + ha2)
	}
	return subtle.ConstantTimeCompare([]byte(p["response"]), []byte(want)) == 1
}

func Answer(challenge, method, uri, user, password string) string {
	scheme, rest, _ := strings.Cut(strings.TrimSpace(challenge), " ")
	if strings.EqualFold(scheme, "Basic") {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
	}
	p := params(rest)
	realm, nonce := p["realm"], p["nonce"]
	ha1 := sum(user + ":" + realm + ":" + password)
	ha2 := sum(method + ":" + uri)
	header := fmt.Sprintf(
		`Digest username="%s", realm="%s", nonce="%s", uri="%s"`,
		user,
		realm,
		nonce,
		uri,
	)
	if qop := p["qop"]; qop != "" {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		cnonce, nc := hex.EncodeToString(b), "00000001"
		response := sum(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":auth:" + ha2)
		header += fmt.Sprintf(`, qop=auth, nc=%s, cnonce="%s", response="%s"`, nc, cnonce, response)
	} else {
		header += fmt.Sprintf(`, response="%s"`, sum(ha1+":"+nonce+":"+ha2))
	}
	if opaque := p["opaque"]; opaque != "" {
		header += fmt.Sprintf(`, opaque="%s"`, opaque)
	}
	return header
}

func params(s string) map[string]string {
	out := map[string]string{}
	for s != "" {
		var key, value string
		key, s, _ = strings.Cut(s, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		s = strings.TrimLeft(s, " ")
		if strings.HasPrefix(s, `"`) {
			value, s, _ = strings.Cut(s[1:], `"`)
			_, s, _ = strings.Cut(s, ",")
		} else {
			value, s, _ = strings.Cut(s, ",")
			value = strings.TrimSpace(value)
		}
		if key != "" {
			out[key] = value
		}
	}
	return out
}

func sum(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}
