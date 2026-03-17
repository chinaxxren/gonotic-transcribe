package repository

import (
	"bufio"
	"fmt"
	"strings"
	"time"

	json "github.com/bytedance/sonic"
)

func parseTranscriptionPayload(content []byte, meetingID int) ([]*TranscriptionRecord, error) {
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return []*TranscriptionRecord{}, nil
	}

	var records []*TranscriptionRecord

	if err := json.Unmarshal([]byte(trimmed), &records); err == nil && len(records) > 0 {
		return records, nil
	}

	var wrapper struct {
		Transcription []*TranscriptionRecord `json:"transcription"`
		Messages      []*TranscriptionRecord `json:"messages"`
	}
	if err := json.Unmarshal([]byte(trimmed), &wrapper); err == nil {
		if len(wrapper.Transcription) > 0 {
			return wrapper.Transcription, nil
		}
		if len(wrapper.Messages) > 0 {
			return wrapper.Messages, nil
		}
	}

	scanner := bufio.NewScanner(strings.NewReader(trimmed))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record TranscriptionRecord
		if err := json.Unmarshal([]byte(line), &record); err == nil {
			records = append(records, &record)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("无法解析转录内容")
	}

	return records, nil
}

func normalizeTranscriptionRecords(records []*TranscriptionRecord, meetingID int) []*TranscriptionRecord {
	now := time.Now().Unix()
	used := make(map[int64]struct{})

	for i := range records {
		if records[i].MeetingID == 0 {
			records[i].MeetingID = meetingID
		}
		if records[i].Timestamp == 0 {
			records[i].Timestamp = now + int64(i)
		}

		for {
			if _, exists := used[records[i].Timestamp]; !exists {
				break
			}
			records[i].Timestamp++
		}
		used[records[i].Timestamp] = struct{}{}
	}

	return records
}
