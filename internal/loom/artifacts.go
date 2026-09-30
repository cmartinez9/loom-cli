package loom

import "context"

// Artifact sections FetchArtifacts can fetch, in the order it fetches them.
const (
	SectionSummary     = "summary"
	SectionChapters    = "chapters"
	SectionActionItems = "action_items"
	SectionTakeaways   = "takeaways"
	SectionComments    = "comments"
	SectionTranscript  = "transcript"
)

// ArtifactOptions picks the sections FetchArtifacts fetches.
type ArtifactOptions struct {
	Summary     bool
	Chapters    bool
	ActionItems bool
	Takeaways   bool
	Comments    bool
	Transcript  bool
}

// Artifacts is one video's fetched sections. A section that was not
// requested, or that failed, is nil.
type Artifacts struct {
	Summary     *Summary
	Chapters    *Chapters
	ActionItems []Task
	Takeaways   *Takeaways
	Comments    []Comment
	Transcript  *Transcript
}

// SectionError is one section that could not be fetched.
type SectionError struct {
	Section string
	Err     error
}

// FetchArtifacts fetches the requested sections of one video, in the order
// of the Section constants. A section that fails is reported in the
// returned section errors and the others are still fetched; an error that
// stops the whole run (IsRunStopping: expired session, rate limit), or the
// end of ctx (no later section could succeed), is returned as the abort
// error, with the artifacts and section errors collected so far. Callers
// decide whether an ended ctx stops their run or fails one video.
func (l *Client) FetchArtifacts(ctx context.Context, id string, opts ArtifactOptions) (*Artifacts, []SectionError, error) {
	a := &Artifacts{}
	var errs []SectionError
	// fail records a section error, or reports true when err stops the run.
	fail := func(section string, err error) bool {
		if IsRunStopping(err) || ctx.Err() != nil {
			return true
		}
		errs = append(errs, SectionError{Section: section, Err: err})
		return false
	}
	if opts.Summary {
		s, err := l.FetchSummary(ctx, id)
		if err != nil && fail(SectionSummary, err) {
			return a, errs, err
		}
		a.Summary = s
	}
	if opts.Chapters {
		c, err := l.FetchChapters(ctx, id)
		if err != nil && fail(SectionChapters, err) {
			return a, errs, err
		}
		a.Chapters = c
	}
	if opts.ActionItems {
		t, err := l.FetchTasks(ctx, id)
		if err != nil && fail(SectionActionItems, err) {
			return a, errs, err
		}
		a.ActionItems = t
	}
	if opts.Takeaways {
		t, err := l.FetchTakeaways(ctx, id)
		if err != nil && fail(SectionTakeaways, err) {
			return a, errs, err
		}
		a.Takeaways = t
	}
	if opts.Comments {
		c, err := l.FetchComments(ctx, id, false)
		if err != nil && fail(SectionComments, err) {
			return a, errs, err
		}
		a.Comments = c
	}
	if opts.Transcript {
		t, err := l.FetchTranscript(ctx, id)
		if err != nil && fail(SectionTranscript, err) {
			return a, errs, err
		}
		a.Transcript = t
	}
	return a, errs, nil
}
