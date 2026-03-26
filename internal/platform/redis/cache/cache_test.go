package cache

import (
	"strings"
	"testing"
	"time"

	redisclient "AstraStorage/internal/platform/redis/client"
)

func TestKeyBuilders_ReturnStableNames(t *testing.T) {
	if got := FileMetaKey("file-1"); got != "astra:cache:file:meta:file-1" {
		t.Fatalf("unexpected file meta key %q", got)
	}
	if got := DownloadPlanKey("file-1"); got != "astra:cache:download:plan:file-1" {
		t.Fatalf("unexpected download plan key %q", got)
	}
	if got := DirectoryListKey("root", 20, 50); got != "astra:cache:dir:list:root:20:50" {
		t.Fatalf("unexpected directory list key %q", got)
	}
	if got := NodeHealthKey("node-a"); got != "astra:cache:node:health:node-a" {
		t.Fatalf("unexpected node health key %q", got)
	}
	if got := NullFileKey("missing-file"); got != "astra:cache:null:file:missing-file" {
		t.Fatalf("unexpected null file key %q", got)
	}
	if got := FileBloomKey(); got != "astra:cache:bf:file" {
		t.Fatalf("unexpected file bloom key %q", got)
	}
}

func TestNewPolicy_UsesGroupDefaults(t *testing.T) {
	cfg := redisclient.ReplicationGroupConfig{
		MasterSetName:     "astra-cache",
		FileMetaTTL:       5 * time.Minute,
		FileMetaTTLJitter: 45 * time.Second,
		DownloadPlanTTL:   3 * time.Minute,
		DirectoryListTTL:  90 * time.Second,
		NodeHealthTTL:     15 * time.Second,
		NullEntryTTL:      30 * time.Second,
	}

	policy := NewPolicy(cfg)
	if policy.FileMetaTTL != 5*time.Minute {
		t.Fatalf("unexpected file meta ttl %s", policy.FileMetaTTL)
	}
	if policy.FileMetaTTLJitter != 45*time.Second {
		t.Fatalf("unexpected file meta ttl jitter %s", policy.FileMetaTTLJitter)
	}
	if policy.NullEntryTTL != 30*time.Second {
		t.Fatalf("unexpected null entry ttl %s", policy.NullEntryTTL)
	}
}

func TestApplyTTLJitter_StaysWithinExpectedWindow(t *testing.T) {
	base := 5 * time.Minute
	jitter := 45 * time.Second

	for i := 0; i < 128; i++ {
		ttl := ApplyTTLJitter(base, jitter)
		if ttl < base-jitter || ttl > base+jitter {
			t.Fatalf("ttl %s out of expected range [%s,%s]", ttl, base-jitter, base+jitter)
		}
	}
}

func TestCodecRoundTrip_PreservesPayload(t *testing.T) {
	type payload struct {
		FileID string `json:"file_id"`
		Status string `json:"status"`
	}

	encoded, err := Encode(payload{FileID: "file-1", Status: "available"})
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	if !strings.Contains(string(encoded), "file-1") {
		t.Fatalf("expected encoded payload to contain file id, got %q", string(encoded))
	}

	var decoded payload
	if err := Decode(encoded, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if decoded.FileID != "file-1" || decoded.Status != "available" {
		t.Fatalf("unexpected decoded payload %#v", decoded)
	}
}
