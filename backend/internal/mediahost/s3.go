package mediahost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// s3Style captures how each object-storage vendor forms its host and region.
// They all speak the same path-style PUT + SigV4 signing, which is why one
// uploader can serve Aliyun OSS, Tencent COS, Cloudflare R2 and AWS S3.
type s3Style struct {
	// hostTemplate receives the bucket (and region/account) to build the
	// endpoint host. Empty placeholders are ignored by callers.
	host func(config Config) (string, error)
	// service is the SigV4 service name used in the credential scope.
	service string
	// signRegion is the region placed in the credential scope.
	signRegion func(config Config) string
}

var (
	s3StyleAliyun = s3Style{
		host: func(config Config) (string, error) {
			if config.Region == "" {
				return "", fmt.Errorf("阿里云 OSS 需要填写区域（例如 oss-cn-hangzhou）")
			}
			region := strings.TrimPrefix(config.Region, "oss-")
			return fmt.Sprintf("%s.%s.aliyuncs.com", config.Bucket, region), nil
		},
		service:    "oss",
		signRegion: func(config Config) string { return strings.TrimPrefix(config.Region, "oss-") },
	}
	s3StyleTencent = s3Style{
		host: func(config Config) (string, error) {
			if config.Region == "" {
				return "", fmt.Errorf("腾讯云 COS 需要填写区域（例如 ap-guangzhou）")
			}
			return fmt.Sprintf("%s.cos.%s.myqcloud.com", config.Bucket, config.Region), nil
		},
		service:    "cos",
		signRegion: func(config Config) string { return config.Region },
	}
	s3StyleCloudflareR2 = s3Style{
		host: func(config Config) (string, error) {
			if config.AccountID == "" {
				return "", fmt.Errorf("Cloudflare R2 需要填写 Account ID")
			}
			return fmt.Sprintf("%s.%s.r2.cloudflarestorage.com", config.Bucket, config.AccountID), nil
		},
		service:    "s3",
		signRegion: func(config Config) string { return "auto" },
	}
	s3StyleAWSS3 = s3Style{
		host: func(config Config) (string, error) {
			region := config.Region
			if region == "" {
				region = "us-east-1"
			}
			return fmt.Sprintf("%s.s3.%s.amazonaws.com", config.Bucket, region), nil
		},
		service: "s3",
		signRegion: func(config Config) string {
			if config.Region == "" {
				return "us-east-1"
			}
			return config.Region
		},
	}
)

const unsignedPayload = "UNSIGNED-PAYLOAD"

// uploadS3Compatible signs and sends a path-style PUT, then returns the public
// URL the object will be readable from.
func (u *Uploader) uploadS3Compatible(ctx context.Context, config Config, filename, contentType string, body []byte, style s3Style) (string, error) {
	host, err := style.host(config)
	if err != nil {
		return "", err
	}
	key := objectKey(config.PathPrefix, kindFromContentType(contentType), filename)

	target := &url.URL{
		Scheme: "https",
		Host:   host,
		Path:   "/" + key,
	}

	now := u.now().UTC()
	region := style.signRegion(config)
	headers := map[string]string{
		"content-type": contentType,
		"host":         host,
		"x-amz-content-sha256": unsignedPayload,
		"x-amz-date":           now.Format("20060102T150405Z"),
	}
	if config.SessionToken != "" {
		headers["x-amz-security-token"] = config.SessionToken
	}

	authorization := signV4(signV4Input{
		method:    http.MethodPut,
		target:    target,
		region:    region,
		service:   style.service,
		headers:   headers,
		accessKey: config.AccessKeyID,
		secretKey: config.SecretAccessKey,
		now:       now,
	})

	request, err := http.NewRequest(http.MethodPut, target.String(), io.NopCloser(strings.NewReader(string(body))))
	if err != nil {
		return "", err
	}
	request.ContentLength = int64(len(body))
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("x-amz-content-sha256", unsignedPayload)
	request.Header.Set("x-amz-date", headers["x-amz-date"])
	if config.SessionToken != "" {
		request.Header.Set("x-amz-security-token", config.SessionToken)
	}
	request.Header.Set("Authorization", authorization)

	response, err := u.do(ctx, request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", fmt.Errorf("对象存储返回 %d：%s", response.StatusCode, responseMessage(payload))
	}

	base := strings.TrimRight(config.PublicBaseURL, "/")
	if base == "" {
		base = "https://" + host
	}
	return base + "/" + key, nil
}

type signV4Input struct {
	method    string
	target    *url.URL
	region    string
	service   string
	headers   map[string]string
	accessKey string
	secretKey string
	now       time.Time
}

// signV4 implements the AWS Signature Version 4 request signing that OSS, COS,
// R2 and S3 all accept.
func signV4(input signV4Input) string {
	const algorithm = "AWS4-HMAC-SHA256"
	dateStamp := input.now.Format("20060102")
	amzDate := input.now.Format("20060102T150405Z")
	scope := strings.Join([]string{dateStamp, input.region, input.service, "aws4_request"}, "/")

	signedNames := make([]string, 0, len(input.headers))
	canonicalHeaders := &strings.Builder{}
	for _, name := range sortedKeys(input.headers) {
		lower := strings.ToLower(name)
		signedNames = append(signedNames, lower)
		canonicalHeaders.WriteString(lower)
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(strings.TrimSpace(input.headers[name]))
		canonicalHeaders.WriteString("\n")
	}
	signedHeaderList := strings.Join(signedNames, ";")

	canonicalRequest := strings.Join([]string{
		input.method,
		escapedPath(input.target),
		canonicalQuery(input.target),
		canonicalHeaders.String(),
		signedHeaderList,
		unsignedPayload,
	}, "\n")

	stringToSign := strings.Join([]string{
		algorithm,
		amzDate,
		scope,
		hex.EncodeToString(sha256Sum([]byte(canonicalRequest))),
	}, "\n")

	dateKey := hmacSHA256([]byte("AWS4"+input.secretKey), dateStamp)
	regionKey := hmacSHA256(dateKey, input.region)
	serviceKey := hmacSHA256(regionKey, input.service)
	signingKey := hmacSHA256(serviceKey, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	return fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		algorithm, input.accessKey, scope, signedHeaderList, signature)
}

func sha256Sum(value []byte) []byte {
	sum := sha256.Sum256(value)
	return sum[:]
}

func sortedKeys(input map[string]string) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && strings.ToLower(keys[j]) < strings.ToLower(keys[j-1]); j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// escapedPath encodes each path segment while leaving separators intact, as
// required by SigV4 canonicalization.
func escapedPath(target *url.URL) string {
	if target == nil {
		return "/"
	}
	segments := strings.Split(target.Path, "/")
	for index, segment := range segments {
		segments[index] = strings.ReplaceAll(url.PathEscape(segment), "+", "%20")
	}
	escaped := strings.Join(segments, "/")
	if escaped == "" {
		return "/"
	}
	return escaped
}

func canonicalQuery(target *url.URL) string {
	if target == nil {
		return ""
	}
	return target.Query().Encode()
}

// kindFromContentType maps a MIME type back to a media kind so object keys land
// under the right prefix.
func kindFromContentType(contentType string) MediaKind {
	lower := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch {
	case strings.HasPrefix(lower, "image/"):
		return MediaKindImage
	case strings.HasPrefix(lower, "video/"):
		return MediaKindVideo
	case strings.HasPrefix(lower, "audio/"):
		return MediaKindAudio
	default:
		return MediaKindImage
	}
}
