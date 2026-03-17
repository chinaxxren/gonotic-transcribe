package service

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestAdvanceResultGenerationDrainsBufferedResults(t *testing.T) {
	session := &WebSocketSession{
		resultQueue:  make(chan *queuedTranscriptionResult, 4),
		resultWakeCh: make(chan struct{}, 1),
		ctx:          context.Background(),
		logger:       zap.NewNop(),
	}

	session.resultQueue <- &queuedTranscriptionResult{Generation: 0}
	session.resultQueue <- &queuedTranscriptionResult{Generation: 0}
	_ = session.enqueueOverflowResult(&queuedTranscriptionResult{Generation: 0, IsFinal: true})

	generation := session.AdvanceResultGeneration()
	if generation != 1 {
		t.Fatalf("expected generation 1, got %d", generation)
	}

	if got := len(session.resultQueue); got != 0 {
		t.Fatalf("expected result queue to be drained, got %d buffered items", got)
	}
	if got := len(session.DrainOverflowResults()); got != 0 {
		t.Fatalf("expected overflow results to be drained, got %d items", got)
	}
}

func TestRequeuePendingFinalRecordsPreservesFailedTail(t *testing.T) {
	session := &WebSocketSession{
		logger: zap.NewNop(),
	}

	if err := session.BufferPendingFinalRecord(&TranscriptionRecord{Text: "newer", MeetingID: 10}); err != nil {
		t.Fatalf("expected initial pending record to buffer, got %v", err)
	}
	if err := session.RequeuePendingFinalRecords([]*TranscriptionRecord{
		{Text: "failed-current", MeetingID: 10},
		{Text: "failed-tail", MeetingID: 10},
	}); err != nil {
		t.Fatalf("expected failed records to requeue, got %v", err)
	}

	records := session.DrainPendingFinalRecords()
	if len(records) != 3 {
		t.Fatalf("expected 3 pending records, got %d", len(records))
	}

	expected := []string{"failed-current", "failed-tail", "newer"}
	for i, want := range expected {
		if records[i] == nil || records[i].Text != want {
			t.Fatalf("expected record %d to be %q, got %#v", i, want, records[i])
		}
	}
}

func TestEnqueueResultUsesOverflowForFinalWhenQueueIsFull(t *testing.T) {
	session := &WebSocketSession{
		resultQueue:  make(chan *queuedTranscriptionResult, 1),
		resultWakeCh: make(chan struct{}, 1),
		ctx:          context.Background(),
		logger:       zap.NewNop(),
	}

	session.resultQueue <- &queuedTranscriptionResult{Text: "queued"}

	ok := session.EnqueueResult(&queuedTranscriptionResult{
		Text:       "final-overflow",
		IsFinal:    true,
		Generation: 1,
	})
	if !ok {
		t.Fatal("expected final result to spill into overflow instead of failing")
	}

	overflow := session.DrainOverflowResults()
	if len(overflow) != 1 {
		t.Fatalf("expected 1 overflow result, got %d", len(overflow))
	}
	if overflow[0] == nil || overflow[0].Text != "final-overflow" {
		t.Fatalf("unexpected overflow payload: %#v", overflow[0])
	}
}

func TestDrainBufferedAudioDropsExpiredPackets(t *testing.T) {
	session := &WebSocketSession{
		logger: zap.NewNop(),
		pendingAudio: []bufferedAudioPacket{
			{
				Data:       []byte("expired"),
				BufferedAt: time.Now().Add(-maxBufferedAudioAge - time.Second),
			},
			{
				Data:       []byte("fresh"),
				BufferedAt: time.Now(),
			},
		},
	}

	audio := session.DrainBufferedAudio()
	if len(audio) != 1 {
		t.Fatalf("expected 1 fresh packet, got %d", len(audio))
	}
	if string(audio[0].Data) != "fresh" {
		t.Fatalf("expected fresh packet, got %q", string(audio[0].Data))
	}
}

func TestRequeueBufferedAudioPreservesPacketAge(t *testing.T) {
	originalBufferedAt := time.Now().Add(-maxBufferedAudioAge + time.Second)
	session := &WebSocketSession{
		logger: zap.NewNop(),
	}

	if err := session.RequeueBufferedAudio([]bufferedAudioPacket{{
		Data:       []byte("requeued"),
		BufferedAt: originalBufferedAt,
	}}); err != nil {
		t.Fatalf("expected requeue to succeed, got %v", err)
	}
	audio := session.DrainBufferedAudio()
	if len(audio) != 1 {
		t.Fatalf("expected requeued packet to remain, got %d", len(audio))
	}
	if string(audio[0].Data) != "requeued" {
		t.Fatalf("expected requeued packet, got %q", string(audio[0].Data))
	}
	if !audio[0].BufferedAt.Equal(originalBufferedAt) {
		t.Fatalf("expected bufferedAt to be preserved, got %v want %v", audio[0].BufferedAt, originalBufferedAt)
	}
}

func TestRequeueBufferedAudioDropsPacketsThatExpireAcrossRetries(t *testing.T) {
	session := &WebSocketSession{
		logger: zap.NewNop(),
	}

	if err := session.RequeueBufferedAudio([]bufferedAudioPacket{{
		Data:       []byte("expired-on-retry"),
		BufferedAt: time.Now().Add(-maxBufferedAudioAge - time.Second),
	}}); err != nil {
		t.Fatalf("expected expired packet requeue to be ignored without error, got %v", err)
	}

	audio := session.DrainBufferedAudio()
	if len(audio) != 0 {
		t.Fatalf("expected expired requeued packet to be dropped, got %d", len(audio))
	}
}

func TestBeginBufferedAudioFlushSerializesConcurrentFlushes(t *testing.T) {
	session := &WebSocketSession{}
	if !session.BeginBufferedAudioFlush() {
		t.Fatal("expected first buffered-audio flush to start")
	}
	if session.BeginBufferedAudioFlush() {
		t.Fatal("expected second buffered-audio flush to be rejected while active")
	}
	session.EndBufferedAudioFlush()
	if !session.BeginBufferedAudioFlush() {
		t.Fatal("expected flush to be allowed again after end")
	}
}

func TestBufferAudioReturnsErrorWhenFull(t *testing.T) {
	session := &WebSocketSession{
		logger: zap.NewNop(),
	}
	session.pendingAudio = make([]bufferedAudioPacket, 0, maxBufferedAudioPackets)
	for i := 0; i < maxBufferedAudioPackets; i++ {
		session.pendingAudio = append(session.pendingAudio, bufferedAudioPacket{
			Data:       []byte("packet"),
			BufferedAt: time.Now(),
		})
	}

	if err := session.BufferAudio([]byte("overflow")); err != ErrBufferedAudioLimitReached {
		t.Fatalf("expected ErrBufferedAudioLimitReached, got %v", err)
	}
}

func TestBufferPendingFinalRecordReturnsErrorWhenFull(t *testing.T) {
	session := &WebSocketSession{
		logger: zap.NewNop(),
	}
	session.pendingFinalRecords = make([]*TranscriptionRecord, 0, maxPendingFinalRecords)
	for i := 0; i < maxPendingFinalRecords; i++ {
		session.pendingFinalRecords = append(session.pendingFinalRecords, &TranscriptionRecord{Text: "existing"})
	}

	if err := session.BufferPendingFinalRecord(&TranscriptionRecord{Text: "overflow"}); err != ErrPendingFinalRecordsFull {
		t.Fatalf("expected ErrPendingFinalRecordsFull, got %v", err)
	}
}

func TestSendMessageDoesNotMutateInputMap(t *testing.T) {
	session := &WebSocketSession{
		logger: zap.NewNop(),
	}
	msg := map[string]interface{}{
		"type":         "transcription",
		"receive_time": int64(123),
	}

	err := session.SendMessage(msg)
	if err == nil {
		t.Fatal("expected missing websocket connection error")
	}
	if _, ok := msg["send_time"]; ok {
		t.Fatal("expected SendMessage to avoid mutating caller map")
	}
}

func TestSendMessageDoesNotMutateFrontendTranscriptionMessage(t *testing.T) {
	session := &WebSocketSession{
		logger: zap.NewNop(),
	}
	msg := &FrontendTranscriptionMessage{
		Type:        MessageTypeTranscription,
		Text:        "hello",
		ReceiveTime: 123,
	}

	err := session.SendMessage(msg)
	if err == nil {
		t.Fatal("expected missing websocket connection error")
	}
	if msg.SendTime != 0 {
		t.Fatal("expected SendMessage to avoid mutating caller struct")
	}
}
