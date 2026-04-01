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
        // 不在此處做流式替換處理，直接將原始 chunk 透傳到 OutputQueue
        // 最終的輸出頻道（WSChannel）會做一次性替換
        // 如果在這裡替換，替換後的值會被 WSChannel 再次處理，導致文字亂序
        sc.session.EnqueueOutput(chunk)
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
