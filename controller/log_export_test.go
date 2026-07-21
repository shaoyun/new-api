package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseUsernames(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "empty", raw: "", want: nil},
		{name: "blanks only", raw: " , , ", want: nil},
		{name: "single", raw: "alice", want: []string{"alice"}},
		{name: "trim and split", raw: " alice , bob ", want: []string{"alice", "bob"}},
		{name: "dedup preserves order", raw: "bob,alice,bob", want: []string{"bob", "alice"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseUsernames(tt.raw))
		})
	}
}

func TestFormatQuotaUSD(t *testing.T) {
	// QuotaPerUnit is process-global and runtime-overridable; pin it for the test.
	orig := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	defer func() { common.QuotaPerUnit = orig }()

	tests := []struct {
		name  string
		quota int
		want  string
	}{
		{name: "zero", quota: 0, want: "0"},
		{name: "trims trailing zeros", quota: 220, want: "0.00044"},
		{name: "whole dollar", quota: 500000, want: "1"},
		{name: "small fraction", quota: 15, want: "0.00003"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatQuotaUSD(tt.quota))
		})
	}
}

func TestOtherIntFromDecodedJSON(t *testing.T) {
	// `other` is stored as JSON text; decoding yields float64 for numbers.
	other, err := common.StrToMap(`{"cache_tokens":128,"cache_creation_tokens":64}`)
	require.NoError(t, err)

	assert.Equal(t, 128, otherInt(other, "cache_tokens"))
	assert.Equal(t, 64, otherInt(other, "cache_creation_tokens"))
	assert.Equal(t, 0, otherInt(other, "missing_key"))
	assert.Equal(t, 0, otherInt(nil, "cache_tokens"))
}
