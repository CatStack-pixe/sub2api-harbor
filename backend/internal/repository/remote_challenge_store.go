package repository

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type remoteChallengeStore struct {
	rdb *redis.Client
}

func NewRemoteChallengeStore(rdb *redis.Client) service.RemoteChallengeStore {
	return &remoteChallengeStore{rdb: rdb}
}

func (s *remoteChallengeStore) Create(ctx context.Context, clientID string, ttl time.Duration) (*service.RemoteChallenge, error) {
	if ttl <= 0 {
		return nil, fmt.Errorf("challenge ttl must be positive")
	}
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return nil, err
	}
	challenge := &service.RemoteChallenge{
		ID:        uuid.NewString(),
		Nonce:     base64.RawURLEncoding.EncodeToString(nonceBytes),
		ExpiresAt: time.Now().UTC().Add(ttl),
	}
	ok, err := s.rdb.SetNX(ctx, remoteChallengeKey(clientID, challenge.ID), challenge.Nonce, ttl).Result()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("challenge collision")
	}
	return challenge, nil
}

func (s *remoteChallengeStore) Get(ctx context.Context, clientID, challengeID string) (string, error) {
	value, err := s.rdb.Get(ctx, remoteChallengeKey(clientID, challengeID)).Result()
	if err == redis.Nil {
		return "", service.ErrRemoteChallengeInvalid
	}
	return value, err
}

var consumeRemoteChallengeScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if not current or current ~= ARGV[1] then
  return 0
end
redis.call('DEL', KEYS[1])
return 1
`)

func (s *remoteChallengeStore) Consume(ctx context.Context, clientID, challengeID, nonce string) (bool, error) {
	result, err := consumeRemoteChallengeScript.Run(ctx, s.rdb, []string{remoteChallengeKey(clientID, challengeID)}, nonce).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func remoteChallengeKey(clientID, challengeID string) string {
	return "remote-ingest:challenge:" + clientID + ":" + challengeID
}
