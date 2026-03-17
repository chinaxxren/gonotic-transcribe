package repository

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTranscriptionPayload_Empty(t *testing.T) {
	recs, err := parseTranscriptionPayload([]byte("\n  \t"), 1)
	require.NoError(t, err)
	require.Empty(t, recs)
}

func TestParseTranscriptionPayload_JSONArray(t *testing.T) {
	content := []byte(`[{"text":"hi","timestamp":1,"uid":1,"meeting_id":0}]`)
	recs, err := parseTranscriptionPayload(content, 99)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	require.Equal(t, "hi", recs[0].Text)
}

func TestParseTranscriptionPayload_WrapperTranscription(t *testing.T) {
	content := []byte(`{"transcription":[{"text":"a","timestamp":1,"uid":1,"meeting_id":0}]}`)
	recs, err := parseTranscriptionPayload(content, 99)
	require.NoError(t, err)
	require.Len(t, recs, 1)
}

func TestParseTranscriptionPayload_JSONL(t *testing.T) {
	content := []byte("{\"text\":\"a\",\"timestamp\":1,\"uid\":1,\"meeting_id\":0}\n{\"text\":\"b\",\"timestamp\":2,\"uid\":1,\"meeting_id\":0}\n")
	recs, err := parseTranscriptionPayload(content, 99)
	require.NoError(t, err)
	require.Len(t, recs, 2)
}

func TestParseTranscriptionPayload_Invalid(t *testing.T) {
	_, err := parseTranscriptionPayload([]byte("not json"), 1)
	require.Error(t, err)
}

func TestNormalizeTranscriptionRecords_FillsMeetingAndUniqueTimestamp(t *testing.T) {
	recs := []*TranscriptionRecord{
		{Text: "a", UID: 1, MeetingID: 0, Timestamp: 0},
		{Text: "b", UID: 1, MeetingID: 0, Timestamp: 0},
		{Text: "c", UID: 1, MeetingID: 7, Timestamp: 10},
		{Text: "d", UID: 1, MeetingID: 0, Timestamp: 10},
	}

	out := normalizeTranscriptionRecords(recs, 99)
	require.Len(t, out, 4)
	for i := range out {
		if i != 2 {
			require.Equal(t, 99, out[i].MeetingID)
		}
		require.NotZero(t, out[i].Timestamp)
	}
	// timestamps should be unique
	seen := map[int64]struct{}{}
	for _, r := range out {
		_, ok := seen[r.Timestamp]
		require.False(t, ok)
		seen[r.Timestamp] = struct{}{}
	}
}
