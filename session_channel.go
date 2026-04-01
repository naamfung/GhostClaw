package main

// SessionChannel 会话输出通道
type SessionChannel struct {
	*BaseChannel
	session *GlobalSession
}

func NewSessionChannel(session *GlobalSession) *SessionChannel {
	return &SessionChannel{
		BaseChannel: NewBaseChannel("session:" + session.ID),
		session:     session,
	}
}

func (sc *SessionChannel) WriteChunk(chunk StreamChunk) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	processed := sc.ProcessChunkWithReplacement(chunk)
	sc.session.EnqueueOutput(processed)
	return nil
}

func (sc *SessionChannel) Close() error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.session.EnqueueOutput(StreamChunk{Done: true})
	return nil
}

func (sc *SessionChannel) GetSessionID() string {
	return sc.session.ID
}
