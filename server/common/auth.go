package common

import (
	"strings"
	"time"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/pkg/cachex"
	"github.com/golang-jwt/jwt/v4"
	"github.com/pkg/errors"
)

var SecretKey []byte

type UserClaims struct {
	Username string `json:"username"`
	PwdTS    int64  `json:"pwd_ts"`
	jwt.RegisteredClaims
}

const invalidatedTokenCacheMaxEntries = 100000

var invalidatedTokenCache = cachex.NewBoundedTTLCache[struct{}](invalidatedTokenCacheMaxEntries, 128)

func GenerateToken(user *model.User) (tokenString string, err error) {
	claim := UserClaims{
		Username: user.Username,
		PwdTS:    user.PwdTS,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(conf.Conf.TokenExpiresIn) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		}}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claim)
	tokenString, err = token.SignedString(SecretKey)
	if err != nil {
		return "", err
	}
	return tokenString, err
}

func parseTokenClaims(tokenString string) (*UserClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &UserClaims{}, func(token *jwt.Token) (interface{}, error) {
		return SecretKey, nil
	})
	if err != nil {
		if ve, ok := err.(*jwt.ValidationError); ok {
			if ve.Errors&jwt.ValidationErrorMalformed != 0 {
				return nil, errors.New("that's not even a token")
			} else if ve.Errors&jwt.ValidationErrorExpired != 0 {
				return nil, errors.New("token is expired")
			} else if ve.Errors&jwt.ValidationErrorNotValidYet != 0 {
				return nil, errors.New("token not active yet")
			} else {
				return nil, errors.New("couldn't handle this token")
			}
		}
	}
	if claims, ok := token.Claims.(*UserClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, errors.New("couldn't handle this token")
}

func ParseToken(tokenString string) (*UserClaims, error) {
	if IsTokenInvalidated(tokenString) {
		return nil, errors.New("token is invalidated")
	}
	return parseTokenClaims(tokenString)
}

func InvalidateToken(tokenString string) error {
	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" {
		return nil // don't invalidate empty guest token
	}
	claims, err := parseTokenClaims(tokenString)
	if err != nil {
		return nil
	}
	if claims.ExpiresAt == nil {
		return nil
	}
	ttl := time.Until(claims.ExpiresAt.Time)
	if ttl <= 0 {
		return nil
	}
	invalidatedTokenCache.Set(tokenString, struct{}{}, ttl)
	return nil
}

func IsTokenInvalidated(tokenString string) bool {
	if strings.TrimSpace(tokenString) == "" {
		return false
	}
	_, ok := invalidatedTokenCache.Get(tokenString)
	return ok
}
