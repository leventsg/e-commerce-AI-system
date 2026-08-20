package rag

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	ragCacheIndex  = "idx:rag:query"
	ragCachePrefix = "rag:query:"
)

type VectorCache struct {
	client    *redis.Client
	dimension int
	threshold float64
	ttl       time.Duration
}

func NewVectorCache(client *redis.Client, cfg config.RAGConfig) *VectorCache {
	dimension := cfg.EmbeddingDimension
	if dimension <= 0 {
		dimension = 1536
	}
	threshold := cfg.VectorSimilarityThreshold
	if threshold <= 0 || threshold > 1 {
		threshold = 0.95
	}
	ttl := time.Duration(cfg.CacheTTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &VectorCache{
		client:    client,
		dimension: dimension,
		threshold: threshold,
		ttl:       ttl,
	}
}

func (c *VectorCache) Get(ctx context.Context, vector []float32) ([]byte, bool) {
	if c == nil || c.client == nil || len(vector) == 0 {
		return nil, false
	}
	if err := c.ensureIndex(ctx); err != nil {
		logx.Errorw("ensure rag vector index failed", logx.Field("err", err))
		return nil, false
	}
	vectorBytes := encodeVector(vector)
	result, err := c.client.Do(ctx,
		"FT.SEARCH", ragCacheIndex,
		"*=>[KNN 1 @query_vector $query_vector AS vector_score]",
		"PARAMS", "2", "query_vector", string(vectorBytes),
		"RETURN", "2", "result", "vector_score",
		"SORTBY", "vector_score",
		"DIALECT", "2",
	).Result()
	if err != nil {
		logx.Errorw("rag vector search failed", logx.Field("err", err))
		return nil, false
	}
	parsed, ok := parseFTSearch(result)
	if !ok || parsed.distance > 1-c.threshold {
		return nil, false
	}
	return []byte(parsed.result), true
}

func (c *VectorCache) Set(ctx context.Context, query string, vector []float32, result []byte) error {
	if c == nil || c.client == nil || len(vector) == 0 {
		return nil
	}
	if err := c.ensureIndex(ctx); err != nil {
		return err
	}
	key := ragCachePrefix + hashKey(query, vector)
	vectorBytes := encodeVector(vector)
	if err := c.client.HSet(ctx, key,
		"query", query,
		"query_vector", string(vectorBytes),
		"result", string(result),
	).Err(); err != nil {
		return err
	}
	return c.client.Expire(ctx, key, c.ttl).Err()
}

func (c *VectorCache) ensureIndex(ctx context.Context) error {
	err := c.client.Do(ctx,
		"FT.CREATE", ragCacheIndex,
		"ON", "HASH",
		"PREFIX", "1", ragCachePrefix,
		"SCHEMA",
		"query_vector", "VECTOR", "HNSW", "6",
		"TYPE", "FLOAT32",
		"DIM", c.dimension,
		"DISTANCE_METRIC", "COSINE",
	).Err()
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "already exists") {
		return nil
	}
	return err
}

type ftSearchEntry struct {
	result   string
	distance float64
}

func parseFTSearch(value any) (ftSearchEntry, bool) {
	items, ok := value.([]interface{})
	if !ok || len(items) < 2 {
		return ftSearchEntry{}, false
	}
	count, ok := items[0].(int64)
	if !ok || count <= 0 {
		return ftSearchEntry{}, false
	}
	entry := ftSearchEntry{distance: math.MaxFloat64}
	for i := 1; i+1 < len(items); i += 2 {
		fields, ok := items[i+1].([]interface{})
		if !ok {
			continue
		}
		for j := 0; j+1 < len(fields); j += 2 {
			key, _ := fields[j].(string)
			switch key {
			case "result":
				entry.result, _ = fields[j+1].(string)
			case "vector_score":
				score, err := strconv.ParseFloat(fmt.Sprint(fields[j+1]), 64)
				if err == nil {
					entry.distance = score
				}
			}
		}
		if entry.result != "" && entry.distance != math.MaxFloat64 {
			return entry, true
		}
	}
	return ftSearchEntry{}, false
}

func encodeVector(vector []float32) []byte {
	buf := make([]byte, len(vector)*4)
	for i, value := range vector {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(value))
	}
	return buf
}

func hashKey(query string, vector []float32) string {
	raw, _ := json.Marshal(map[string]any{
		"query":  query,
		"dim":    len(vector),
		"vector": vector,
	})
	return fmt.Sprintf("%x", raw)
}
