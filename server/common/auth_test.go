package common

import (
	"testing"
	"time"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/model"
)

func TestTokenInvalidationFlow(t *testing.T) {
	oldSecret := SecretKey
	oldConf := conf.Conf
	defer func() {
		SecretKey = oldSecret
		conf.Conf = oldConf
		invalidatedTokenCache.Clear()
	}()

	SecretKey = []byte("test-secret")
	conf.Conf = &conf.Config{TokenExpiresIn: 1}
	invalidatedTokenCache.Clear()

	user := &model.User{Username: "alice", PwdTS: time.Now().Unix()}
	token, err := GenerateToken(user)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	if IsTokenInvalidated(token) {
		t.Fatalf("fresh token should not be invalidated")
	}

	claims, err := ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken failed before logout: %v", err)
	}
	if claims.Username != user.Username {
		t.Fatalf("unexpected username: %s", claims.Username)
	}

	if err := InvalidateToken(token); err != nil {
		t.Fatalf("InvalidateToken failed: %v", err)
	}
	if !IsTokenInvalidated(token) {
		t.Fatalf("token should be marked invalidated after logout")
	}
	if _, err := ParseToken(token); err == nil {
		t.Fatalf("expected ParseToken to reject invalidated token")
	}
}

func TestInvalidateTokenIgnoresMalformedToken(t *testing.T) {
	if err := InvalidateToken("not-a-token"); err != nil {
		t.Fatalf("InvalidateToken should ignore malformed token, got: %v", err)
	}
}
