package s3server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	s3AuthAlgorithm  = "AWS4-HMAC-SHA256"
	s3RequestScope   = "aws4_request"
	s3ServiceName    = "s3"
	s3PresignMaxSkew = 15 * time.Minute
	s3TimeLayout     = "20060102T150405Z"
	awsXMLTimeLayout = "2006-01-02T15:04:05.000Z"
)

type credentialScope struct {
	AccessKeyID string
	Date        string
	Region      string
	Service     string
	RequestType string
}

type s3AuthInfo struct {
	Credential    string
	Signature     string
	SignedHeaders string
	Algorithm     string
	Expires       string
	SignTime      time.Time
	Scope         credentialScope
	IsPresign     bool
}

func verifyRequestSignature(r *http.Request, body io.ReadSeeker, cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("missing s3 config")
	}
	if r == nil {
		return fmt.Errorf("missing request")
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return fmt.Errorf("missing s3 credentials")
	}

	info, err := parseS3AuthInfo(r)
	if err != nil {
		return err
	}
	if err := validateS3AuthInfo(info, cfg); err != nil {
		return err
	}
	if err := validateS3RequestTime(info); err != nil {
		return err
	}
	return verifyS3Signature(r, body, cfg, info)
}

func parseS3AuthInfo(r *http.Request) (s3AuthInfo, error) {
	if r == nil {
		return s3AuthInfo{}, fmt.Errorf("missing request")
	}
	if sig := strings.TrimSpace(r.URL.Query().Get("X-Amz-Signature")); sig != "" {
		return parsePresignedAuthInfo(r)
	}
	return parseHeaderSignedAuthInfo(r)
}

func parseHeaderSignedAuthInfo(r *http.Request) (s3AuthInfo, error) {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return s3AuthInfo{}, fmt.Errorf("missing authorization")
	}
	if !strings.HasPrefix(strings.ToUpper(auth), s3AuthAlgorithm) {
		return s3AuthInfo{}, fmt.Errorf("invalid authorization algorithm")
	}

	fields, err := parseAuthHeaderFields(auth)
	if err != nil {
		return s3AuthInfo{}, err
	}
	credential := fields["credential"]
	signature := fields["signature"]
	signedHeaders := fields["signedheaders"]
	if credential == "" {
		return s3AuthInfo{}, fmt.Errorf("missing credential")
	}
	if signature == "" {
		return s3AuthInfo{}, fmt.Errorf("missing signature")
	}
	if signedHeaders == "" {
		return s3AuthInfo{}, fmt.Errorf("missing signed headers")
	}
	scope, err := parseCredentialScope(credential)
	if err != nil {
		return s3AuthInfo{}, err
	}
	signTime, err := parseAmzTime(r.Header.Get("X-Amz-Date"))
	if err != nil {
		return s3AuthInfo{}, err
	}
	return s3AuthInfo{
		Credential:    credential,
		Signature:     normalizeSignatureHeader(signature),
		SignedHeaders: normalizeSignedHeaders(signedHeaders),
		Algorithm:     s3AuthAlgorithm,
		SignTime:      signTime,
		Scope:         scope,
	}, nil
}

func parsePresignedAuthInfo(r *http.Request) (s3AuthInfo, error) {
	q := r.URL.Query()
	credential, err := url.PathUnescape(strings.TrimSpace(q.Get("X-Amz-Credential")))
	if err != nil {
		return s3AuthInfo{}, err
	}
	signature := strings.TrimSpace(q.Get("X-Amz-Signature"))
	if signature == "" {
		return s3AuthInfo{}, fmt.Errorf("missing signature")
	}
	signedHeaders := normalizeSignedHeaders(q.Get("X-Amz-SignedHeaders"))
	if signedHeaders == "" {
		return s3AuthInfo{}, fmt.Errorf("missing signed headers")
	}
	scope, err := parseCredentialScope(credential)
	if err != nil {
		return s3AuthInfo{}, err
	}
	signTime, err := parseAmzTime(q.Get("X-Amz-Date"))
	if err != nil {
		return s3AuthInfo{}, err
	}
	return s3AuthInfo{
		Credential:    credential,
		Signature:     normalizeSignatureHeader(signature),
		SignedHeaders: signedHeaders,
		Algorithm:     strings.TrimSpace(q.Get("X-Amz-Algorithm")),
		Expires:       strings.TrimSpace(q.Get("X-Amz-Expires")),
		SignTime:      signTime,
		Scope:         scope,
		IsPresign:     true,
	}, nil
}

func validateS3AuthInfo(info s3AuthInfo, cfg *Config) error {
	if info.Credential == "" {
		return fmt.Errorf("missing credential")
	}
	if info.Algorithm != s3AuthAlgorithm {
		return fmt.Errorf("invalid authorization algorithm")
	}
	if err := verifyCredentialScope(info.Scope, cfg); err != nil {
		return err
	}
	if info.IsPresign {
		if info.Expires == "" {
			return fmt.Errorf("missing expires")
		}
		expires, err := strconv.Atoi(info.Expires)
		if err != nil {
			return fmt.Errorf("invalid expires: %w", err)
		}
		if expires <= 0 || expires > 604800 {
			return fmt.Errorf("invalid expires")
		}
		return nil
	}
	if info.SignTime.IsZero() {
		return fmt.Errorf("missing x-amz-date")
	}
	return nil
}

func validateS3RequestTime(info s3AuthInfo) error {
	if info.SignTime.IsZero() {
		return nil
	}
	if info.IsPresign {
		exp, err := time.ParseDuration(info.Expires + "s")
		if err != nil {
			return fmt.Errorf("invalid expires: %w", err)
		}
		now := time.Now().UTC()
		if now.Before(info.SignTime.Add(-s3PresignMaxSkew)) {
			return fmt.Errorf("signature not yet valid")
		}
		if now.After(info.SignTime.Add(exp).Add(s3PresignMaxSkew)) {
			return fmt.Errorf("signature expired")
		}
		return nil
	}
	age := time.Since(info.SignTime)
	if age < -s3PresignMaxSkew || age > s3PresignMaxSkew {
		return fmt.Errorf("signature expired")
	}
	return nil
}

func verifyS3Signature(r *http.Request, body io.ReadSeeker, cfg *Config, info s3AuthInfo) error {
	candidates, err := payloadHashCandidates(r, body, info)
	if err != nil {
		return err
	}
	for _, payloadHash := range candidates {
		expected, err := buildExpectedSignature(r, cfg, info, payloadHash)
		if err != nil {
			return err
		}
		if strings.EqualFold(expected, info.Signature) {
			return nil
		}
	}
	return fmt.Errorf("signature verification failed")
}

func buildExpectedSignature(r *http.Request, cfg *Config, info s3AuthInfo, payloadHash string) (string, error) {
	canonicalQuery, err := canonicalQueryString(r.URL.RawQuery, info.IsPresign)
	if err != nil {
		return "", err
	}
	canonicalURI := canonicalURIPath(r.URL)
	canonicalHeaders, signedHeaders, err := canonicalHeaders(r, info)
	if err != nil {
		return "", err
	}
	canonicalRequest := strings.Join([]string{
		r.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders + "\n",
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := buildSigningScope(info.Scope.Region, info.Scope.Service, info.SignTime)
	stringToSign := strings.Join([]string{
		s3AuthAlgorithm,
		info.SignTime.UTC().Format(s3TimeLayout),
		scope,
		hex.EncodeToString(sha256Sum([]byte(canonicalRequest))),
	}, "\n")
	signingKey := deriveSigningKey(cfg.SecretAccessKey, info.SignTime, info.Scope.Region, info.Scope.Service)
	return hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign))), nil
}

func payloadHashCandidates(r *http.Request, body io.ReadSeeker, info s3AuthInfo) ([]string, error) {
	if headerHash := strings.TrimSpace(r.Header.Get("X-Amz-Content-Sha256")); headerHash != "" {
		return []string{headerHash}, nil
	}
	if queryHash := strings.TrimSpace(r.URL.Query().Get("X-Amz-Content-Sha256")); queryHash != "" {
		return []string{queryHash}, nil
	}
	if info.IsPresign {
		if body != nil {
			actualHash, err := sha256HexFromSeeker(body)
			if err != nil {
				return nil, err
			}
			_, _ = body.Seek(0, io.SeekStart)
			return []string{"UNSIGNED-PAYLOAD", actualHash}, nil
		}
		return []string{"UNSIGNED-PAYLOAD", emptyStringSHA256()}, nil
	}
	if body != nil {
		actualHash, err := sha256HexFromSeeker(body)
		if err != nil {
			return nil, err
		}
		_, _ = body.Seek(0, io.SeekStart)
		return []string{actualHash}, nil
	}
	return []string{emptyStringSHA256()}, nil
}

func canonicalQueryString(rawQuery string, isPresign bool) (string, error) {
	if rawQuery == "" {
		return "", nil
	}
	type pair struct {
		key   string
		value string
	}
	pairs := make([]pair, 0)
	for _, part := range strings.Split(rawQuery, "&") {
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		key, err := url.QueryUnescape(kv[0])
		if err != nil {
			return "", err
		}
		if isPresign && strings.EqualFold(key, "X-Amz-Signature") {
			continue
		}
		value := ""
		if len(kv) > 1 {
			value, err = url.QueryUnescape(kv[1])
			if err != nil {
				return "", err
			}
		}
		pairs = append(pairs, pair{
			key:   awsQueryEscape(key),
			value: awsQueryEscape(value),
		})
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].key == pairs[j].key {
			return pairs[i].value < pairs[j].value
		}
		return pairs[i].key < pairs[j].key
	})
	var b strings.Builder
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(p.key)
		b.WriteByte('=')
		b.WriteString(p.value)
	}
	return b.String(), nil
}

func canonicalURIPath(u *url.URL) string {
	if u == nil {
		return "/"
	}
	if escaped := u.EscapedPath(); escaped != "" {
		return escaped
	}
	return "/"
}

func canonicalHeaders(r *http.Request, info s3AuthInfo) (string, string, error) {
	signedHeaderNames := splitSignedHeaders(info.SignedHeaders)
	if len(signedHeaderNames) == 0 {
		return "", "", fmt.Errorf("missing signed headers")
	}
	values := make([]string, 0, len(signedHeaderNames))
	for _, headerName := range signedHeaderNames {
		switch headerName {
		case "host":
			host := strings.TrimSpace(r.Host)
			if host == "" && r.URL != nil {
				host = strings.TrimSpace(r.URL.Host)
			}
			if host == "" {
				return "", "", fmt.Errorf("missing host header")
			}
			values = append(values, "host:"+normalizeHeaderValue(host))
			continue
		}

		raw := r.Header.Values(http.CanonicalHeaderKey(headerName))
		if len(raw) == 0 && strings.EqualFold(headerName, "x-amz-content-sha256") {
			if v := strings.TrimSpace(r.URL.Query().Get("X-Amz-Content-Sha256")); v != "" {
				raw = []string{v}
			}
		}
		if len(raw) == 0 {
			return "", "", fmt.Errorf("missing signed header: %s", headerName)
		}
		normalized := make([]string, 0, len(raw))
		for _, v := range raw {
			normalized = append(normalized, normalizeHeaderValue(v))
		}
		values = append(values, headerName+":"+strings.Join(normalized, ","))
	}
	sort.Strings(values)
	return strings.Join(values, "\n"), strings.Join(signedHeaderNames, ";"), nil
}

func splitSignedHeaders(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ";")
	headers := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			headers = append(headers, part)
		}
	}
	sort.Strings(headers)
	return headers
}

func parseAuthHeaderFields(auth string) (map[string]string, error) {
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid authorization header")
	}
	fields := make(map[string]string)
	for _, chunk := range strings.Split(parts[1], ",") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		kv := strings.SplitN(chunk, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid authorization header")
		}
		fields[strings.ToLower(strings.TrimSpace(kv[0]))] = strings.TrimSpace(kv[1])
	}
	return fields, nil
}

func normalizeSignedHeaders(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, ";")
	headers := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			headers = append(headers, part)
		}
	}
	sort.Strings(headers)
	return strings.Join(headers, ";")
}

func normalizeSignatureHeader(v string) string {
	v = strings.TrimSpace(v)
	return strings.ReplaceAll(v, " ", "")
}

func normalizeHeaderValue(v string) string {
	v = strings.TrimSpace(v)
	return strings.Join(strings.Fields(v), " ")
}

func parseAmzTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("missing x-amz-date")
	}
	return time.Parse(s3TimeLayout, raw)
}

func parseCredentialScope(raw string) (credentialScope, error) {
	raw = strings.TrimSpace(raw)
	parts := strings.SplitN(raw, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return credentialScope{}, fmt.Errorf("invalid credential scope")
	}
	scopeParts := strings.Split(parts[1], "/")
	if len(scopeParts) != 4 {
		return credentialScope{}, fmt.Errorf("invalid credential scope")
	}
	return credentialScope{
		AccessKeyID: parts[0],
		Date:        scopeParts[0],
		Region:      scopeParts[1],
		Service:     scopeParts[2],
		RequestType: scopeParts[3],
	}, nil
}

func verifyCredentialScope(scope credentialScope, cfg *Config) error {
	if scope.AccessKeyID != cfg.AccessKeyID {
		return fmt.Errorf("invalid access key")
	}
	if scope.Service != s3ServiceName {
		return fmt.Errorf("invalid service")
	}
	if scope.RequestType != s3RequestScope {
		return fmt.Errorf("invalid request type")
	}
	return nil
}

func buildSigningScope(region, service string, signTime time.Time) string {
	if region == "" {
		region = "us-east-1"
	}
	if service == "" {
		service = s3ServiceName
	}
	return strings.Join([]string{
		signTime.UTC().Format("20060102"),
		region,
		service,
		s3RequestScope,
	}, "/")
}

func deriveSigningKey(secretKey string, signTime time.Time, region, service string) []byte {
	dateKey := hmacSHA256([]byte("AWS4"+secretKey), []byte(signTime.UTC().Format("20060102")))
	regionKey := hmacSHA256(dateKey, []byte(region))
	serviceKey := hmacSHA256(regionKey, []byte(service))
	return hmacSHA256(serviceKey, []byte(s3RequestScope))
}

func emptyStringSHA256() string {
	return "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
}

func sha256HexFromSeeker(rs io.ReadSeeker) (string, error) {
	if rs == nil {
		return emptyStringSHA256(), nil
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(h, rs); err != nil {
		return "", err
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sha256Sum(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write(data)
	return h.Sum(nil)
}

func awsQueryEscape(v string) string {
	return strings.ReplaceAll(url.QueryEscape(v), "+", "%20")
}
