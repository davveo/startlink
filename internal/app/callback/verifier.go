package callback

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/starlink/push/internal/config"
	"github.com/starlink/push/pkg/errcode"
)

// Verifier 校验回执 HMAC，并通过 Redis nonce 防止有效请求被重放。
// 签名原文为 timestamp + "\n" + nonce + "\n" + rawBody。
type Verifier struct {
	required bool
	secret   []byte
	maxSkew  time.Duration
	rdb      redis.Cmdable
}

func NewVerifier(cfg config.CallbackConfig, rdb redis.Cmdable) *Verifier {
	return &Verifier{
		required: cfg.SignatureRequired,
		secret:   []byte(cfg.SignatureSecret),
		maxSkew:  time.Duration(cfg.MaxSkewSec) * time.Second,
		rdb:      rdb,
	}
}

func (v *Verifier) Verify(ctx context.Context, timestamp, nonce, signature string, body []byte) error {
	if v == nil || !v.required {
		return nil
	}
	if len(v.secret) < 32 || len(nonce) < 8 || len(nonce) > 128 {
		return errcode.Unauthorized
	}
	unixSec, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errcode.Unauthorized
	}
	requestTime := time.Unix(unixSec, 0)
	if delta := time.Since(requestTime); delta > v.maxSkew || delta < -v.maxSkew {
		return errcode.Unauthorized
	}

	mac := hmac.New(sha256.New, v.secret)
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write([]byte(nonce))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write(body)
	expected := mac.Sum(nil)
	signature = strings.TrimPrefix(strings.TrimSpace(signature), "sha256=")
	actual, err := hex.DecodeString(signature)
	if err != nil || !hmac.Equal(expected, actual) {
		return errcode.Unauthorized
	}
	if v.rdb == nil {
		return fmt.Errorf("callback replay store unavailable")
	}
	nonceHash := sha256.Sum256([]byte(nonce))
	key := "starlink:callback:nonce:" + hex.EncodeToString(nonceHash[:])
	ok, err := v.rdb.SetNX(ctx, key, "1", 2*v.maxSkew).Result()
	if err != nil {
		return fmt.Errorf("store callback nonce: %w", err)
	}
	if !ok {
		return errcode.Unauthorized
	}
	return nil
}
