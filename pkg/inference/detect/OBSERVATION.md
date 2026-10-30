# Observing wake decisions

`Engine` accepts optional synchronous callbacks for applications that need to
investigate false activations. The library does not record audio, create files,
change thresholds, or send diagnostic data anywhere.

Configure callbacks before `Start`/`Run` and do not replace them until `Close`.
Slots are zero-based. Nil callbacks preserve the existing behavior.

| Callback | Meaning |
| --- | --- |
| `OnModel(slot, model)` | A model successfully loaded. Reusing an already loaded model does not notify again. Called under the engine lock; setup work is allowed, but do not re-enter the engine. |
| `OnFrame(frame, paused, dropped)` | Borrowed PCM before scoring, including paused frames. `dropped` is the microphone source's cumulative dropped-frame count. |
| `OnScore(slot, score, cutoff, crossing, suppressed, dropped)` | A fresh model score. `crossing` identifies the frame that starts detection; `suppressed` identifies refractory frames. Scores during the hold window are still reported. |
| `OnPeak(slot, peak)` | The completed hold-window peak. This is diagnostic information, not another wake decision. |

Audio-path callbacks must return promptly: no disk/network I/O, waiting for a
consumer, or calls back into the engine. Never modify the supplied frame or keep
its slice after the callback returns; copy samples into a bounded buffer if
needed. The caller owns synchronization, overload/drop handling, and privacy
policy. A slow observer can delay scoring, so observation is not an isolation
boundary. With nil hooks there is no diagnostic buffering or extra goroutine.

`OnFrame` runs before the score callbacks for that frame. Frames do not imply
scores: a paused engine does not score, and a backend may score less frequently
than the microphone produces frames. A changed drop counter indicates a known
source discontinuity; it is not an exact sample count or timestamp. Consumers
should discard incomplete captures instead of joining across a gap.

`OnPeak` may be absent when inference pauses, a model changes, or the engine
stops before its hold completes. Neither callback values nor audio snapshots
prove that a user spoke the wake word.

## Conversation source categories

`satellite.Options.OnTrace` is passed to the conversation and also observes API
subscription transitions. Its fixed categories are `api_subscribed`,
`api_unsubscribed`, `turn_start`, and `follow_up`. API events use slot `-1`;
accepted turns use a zero-based slot. Ignored or muted starts do not produce an
accepted-turn trace. No transcript, reply, URL, credential, or audio is passed.

Set the callback before constructing the satellite. Subscription and conversation
callbacks can run concurrently; return promptly and do not re-enter the caller.
Applications can combine these categories with their own button/API markers to
distinguish model detections from other ways of starting a conversation.

## Tests

```sh
go test ./pkg/inference/detect ./pkg/assistant/turn ./pkg/assistant/satellite
go test -race ./pkg/inference/detect ./pkg/assistant/turn ./pkg/assistant/satellite
```

The observation tests use synthetic frames, scores and fake backends. They compare
decision state and detection counts with/without hooks, exercise hold/refractory
boundaries, frame ordering, pause handling, model notifications, subscription
transitions, accepted follow-ups and muted starts. They need no recordings,
model downloads, credentials, devices, or network services.
