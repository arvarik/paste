package api

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestValidateItemOptions(t *testing.T) {
	ConfigureItemFeatures(ItemFeatureConfig{DefaultExpiry: time.Hour, MaximumExpiry: 24 * time.Hour})
	t.Cleanup(func() { ConfigureItemFeatures(ItemFeatureConfig{}) })

	tags, expiry, err := validateItemOptions([]string{"Go", " go ", "review"}, nil, true)
	if err != nil {
		t.Fatalf("validateItemOptions() error = %v", err)
	}
	if len(tags) != 2 || tags[0] != "Go" || expiry == nil {
		t.Fatalf("validateItemOptions() = %#v, %v", tags, expiry)
	}
	if expiry.Before(time.Now().Add(59 * time.Minute)) {
		t.Fatalf("default expiry = %v", expiry)
	}

	past := time.Now().Add(-time.Minute)
	if _, _, err := validateItemOptions(nil, &past, true); err == nil {
		t.Fatal("validateItemOptions() accepted an expired timestamp")
	}
	beyondMaximum := time.Now().Add(25 * time.Hour)
	if _, _, err := validateItemOptions(nil, &beyondMaximum, true); err == nil {
		t.Fatal("validateItemOptions() accepted an expiry beyond the maximum")
	}
}

func TestParsePageRequest(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/pastes?cursor=next&limit=25", nil)
	cursor, limit, err := parsePageRequest(request)
	if err != nil || cursor != "next" || limit != 25 {
		t.Fatalf("parsePageRequest() = %q, %d, %v", cursor, limit, err)
	}

	request = httptest.NewRequest("GET", "/api/pastes?limit=251", nil)
	if _, _, err := parsePageRequest(request); err == nil {
		t.Fatal("parsePageRequest() accepted a page larger than 250")
	}
}
