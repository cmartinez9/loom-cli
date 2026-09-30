package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// TranscriptInfo is Loom's transcript record (fetchVideoTranscript). The
// URLs are short-lived CloudFront-signed links on cdn.loom.com.
type TranscriptInfo struct {
	VideoID     string `json:"video_id"`
	Version     int    `json:"version"`
	Status      string `json:"status"`
	Language    string `json:"language,omitempty"`
	SourceURL   string `json:"-"`
	CaptionsURL string `json:"-"`
}

// Segment is one transcript phrase with timing.
type Segment struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Speaker int     `json:"speaker"`
	Text    string  `json:"text"`
}

// Transcript is a parsed transcript.
type Transcript struct {
	VideoID  string    `json:"video_id"`
	Version  int       `json:"version"`
	Language string    `json:"language,omitempty"`
	Status   string    `json:"status"`
	Segments []Segment `json:"segments"`
}

// Text joins every phrase into one paragraph-per-phrase string.
func (t *Transcript) Text() string {
	parts := make([]string, 0, len(t.Segments))
	for _, s := range t.Segments {
		parts = append(parts, s.Text)
	}
	return strings.Join(parts, "\n")
}

// TranscriptInfo fetches the transcript record for a video.
func (l *Client) TranscriptInfo(ctx context.Context, id string) (*TranscriptInfo, error) {
	data, err := l.Query(ctx, OpTranscript, map[string]any{"videoId": id})
	if err != nil {
		return nil, err
	}
	if l.IsDryRun() {
		return &TranscriptInfo{VideoID: id}, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("%s: %w", OpTranscript, err)
	}
	raw := obj["fetchVideoTranscript"]
	if len(raw) == 0 || string(raw) == "null" {
		return nil, ErrNoTranscript
	}
	if err := CheckUnion(OpTranscript, raw); err != nil {
		return nil, l.normalizeAuth(ctx, err)
	}
	var r struct {
		VideoID     string `json:"video_id"`
		Version     int    `json:"version"`
		SourceURL   string `json:"source_url"`
		CaptionsURL string `json:"captions_source_url"`
		Status      string `json:"transcription_status"`
		Language    string `json:"language"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", OpTranscript, err)
	}
	if r.VideoID == "" {
		r.VideoID = id
	}
	return &TranscriptInfo{VideoID: r.VideoID, Version: r.Version, Status: r.Status, Language: r.Language, SourceURL: r.SourceURL, CaptionsURL: r.CaptionsURL}, nil
}

// FetchTranscript returns the parsed transcript for a video. When Loom is
// still transcribing, it returns a Transcript with no segments and the
// pending status rather than an error.
func (l *Client) FetchTranscript(ctx context.Context, id string) (*Transcript, error) {
	info, err := l.TranscriptInfo(ctx, id)
	if err != nil {
		return nil, err
	}
	t := &Transcript{VideoID: info.VideoID, Version: info.Version, Language: info.Language, Status: info.Status, Segments: make([]Segment, 0)}
	if l.IsDryRun() {
		return t, nil
	}
	if info.SourceURL == "" {
		if info.Status == "" || strings.EqualFold(info.Status, "success") {
			return nil, ErrNoTranscript
		}
		return t, nil
	}
	body, err := l.FetchCDN(ctx, info.SourceURL)
	if err != nil {
		return nil, err
	}
	segs, err := ParsePhrases(body)
	if err != nil {
		return nil, err
	}
	t.Segments = segs
	return t, nil
}

// FetchCaptions returns the WebVTT captions for a video.
func (l *Client) FetchCaptions(ctx context.Context, id string) (string, error) {
	info, err := l.TranscriptInfo(ctx, id)
	if err != nil {
		return "", err
	}
	if l.IsDryRun() {
		return "", nil
	}
	if info.CaptionsURL == "" {
		return "", ErrNoTranscript
	}
	body, err := l.FetchCDN(ctx, info.CaptionsURL)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// ParsePhrases parses Loom's transcript JSON ({phrases:[{ts,value,ranges}]}).
// A phrase has only a start time, so each end is the next phrase's start;
// the last phrase ends a reading-speed estimate after it starts.
func ParsePhrases(body []byte) ([]Segment, error) {
	var doc struct {
		Phrases []struct {
			TS     float64 `json:"ts"`
			Value  string  `json:"value"`
			Ranges []struct {
				Source struct {
					Monologue int `json:"monologue"`
				} `json:"source"`
			} `json:"ranges"`
		} `json:"phrases"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("decoding transcript JSON: %w", err)
	}
	segs := make([]Segment, 0, len(doc.Phrases))
	for _, p := range doc.Phrases {
		text := strings.TrimSpace(p.Value)
		if text == "" {
			continue
		}
		speaker := 0
		if len(p.Ranges) > 0 {
			speaker = p.Ranges[0].Source.Monologue
		}
		segs = append(segs, Segment{Start: round3(p.TS), Speaker: speaker, Text: text})
	}
	for i := range segs {
		if i+1 < len(segs) {
			segs[i].End = segs[i+1].Start
		} else {
			words := len(strings.Fields(segs[i].Text))
			segs[i].End = round3(segs[i].Start + float64(words)*0.4 + 0.5)
		}
		if segs[i].End < segs[i].Start {
			segs[i].End = segs[i].Start
		}
	}
	return segs, nil
}

// FormatTimestamped renders "[m:ss] text" lines.
func FormatTimestamped(segs []Segment) string {
	var b strings.Builder
	for _, s := range segs {
		fmt.Fprintf(&b, "[%s] %s\n", FormatClock(s.Start), s.Text)
	}
	return b.String()
}

// FormatSRT renders SubRip subtitles.
func FormatSRT(segs []Segment) string {
	var b strings.Builder
	for i, s := range segs {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", i+1, srtTime(s.Start, ","), srtTime(s.End, ","), s.Text)
	}
	return b.String()
}

// FormatVTT renders WebVTT subtitles from segments.
func FormatVTT(segs []Segment) string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for i, s := range segs {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", i+1, srtTime(s.Start, "."), srtTime(s.End, "."), s.Text)
	}
	return b.String()
}

func srtTime(sec float64, sep string) string {
	if sec < 0 {
		sec = 0
	}
	ms := int64(sec*1000 + 0.5)
	h := ms / 3600000
	m := (ms % 3600000) / 60000
	s := (ms % 60000) / 1000
	return fmt.Sprintf("%02d:%02d:%02d%s%03d", h, m, s, sep, ms%1000)
}
