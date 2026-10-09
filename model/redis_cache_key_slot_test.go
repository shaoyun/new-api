package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// redisClusterSlot mirrors Redis Cluster key-slot assignment: CRC16-XMODEM of
// the hash tag (first "{...}" with a non-empty body) or of the whole key,
// modulo 16384.
func redisClusterSlot(key string) int {
	if start := strings.IndexByte(key, '{'); start >= 0 {
		if end := strings.IndexByte(key[start+1:], '}'); end > 0 {
			key = key[start+1 : start+1+end]
		}
	}
	var crc uint16
	for i := 0; i < len(key); i++ {
		crc ^= uint16(key[i]) << 8
		for j := 0; j < 8; j++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return int(crc) % 16384
}

// Keys passed together to one Lua script or MGET must hash to one slot, or
// Redis Cluster / ElastiCache Serverless rejects the command with CROSSSLOT.
func TestRedisCacheKeysUsedTogetherShareSlot(t *testing.T) {
	for _, userID := range []int{1, 42, 123456} {
		cacheSlot := redisClusterSlot(getUserCacheKey(userID))
		assert.Equal(t, cacheSlot, redisClusterSlot(getUserAuthFenceKey(userID)), "user %d fence key", userID)
		assert.Equal(t, cacheSlot, redisClusterSlot(getUserAuthVersionKey(userID)), "user %d version key", userID)
	}
	assert.NotEqual(t, redisClusterSlot(getUserCacheKey(1)), redisClusterSlot(getUserCacheKey(2)),
		"hash tag must be per user, not one global slot")

	for _, tokenKey := range []string{"sk-alpha", "sk-beta-0123456789"} {
		assert.Equal(t, redisClusterSlot(getTokenCacheKey(tokenKey)), redisClusterSlot(getTokenCacheFenceKey(tokenKey)),
			"token %q fence key", tokenKey)
	}
}

func TestRedisClusterSlotMatchesReference(t *testing.T) {
	// Canonical vector from the Redis Cluster spec (also go-redis's hashtag test).
	assert.Equal(t, 12739, redisClusterSlot("123456789"))
	// Only the first non-empty "{...}" is hashed.
	assert.Equal(t, redisClusterSlot("bar"), redisClusterSlot("foo{bar}"))
	assert.Equal(t, redisClusterSlot("bar"), redisClusterSlot("foo{bar}{zap}"))
}
