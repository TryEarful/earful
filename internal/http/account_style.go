package http

import (
	"errors"
	"net/http"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/uitext"
	"github.com/TryEarful/earful/web/templates"
)

// accountStyleImages is where the account's style page fetches its
// pictures from: the account's own, which no address serves to anybody
// else.
const accountStyleImages = "/account/style-image/"

// accountStylePage is the account's style (ADR-0023): the theme, header,
// footer and thanks picture every survey of the workspace follows until
// it makes a part its own. A save redirects here and names its outcome
// in the query, as the Style tab's does.
func (s *server) accountStylePage(w http.ResponseWriter, r *http.Request) {
	notice := ""
	if r.URL.Query().Get("notice") == "saved" {
		notice = say(r, "account.style.notice.saved")
	}
	s.renderAccountStyle(w, r, nil, nil, notice, false)
}

// renderAccountStyle draws the account's style page, with what was typed
// and why it was refused when a save was, as renderSurveyStyle does.
func (s *server) renderAccountStyle(w http.ResponseWriter, r *http.Request, typed *domain.Style, problem error, notice string, filesLost bool) {
	info, _ := authFrom(r.Context())
	current, err := s.surveys.WorkspaceStyle(r.Context(), info.WorkspaceID)
	if err != nil {
		s.internalError(w, r, "load workspace style", err)
		return
	}
	data := templates.AccountStyleData{
		StyleForm: templates.StyleForm{Style: current.Style, Account: true},
		Notice:    notice,
	}
	status := http.StatusOK
	if problem != nil {
		status = http.StatusUnprocessableEntity
		data.StyleForm = refusedStyleForm(r, data.StyleForm, typed, problem, filesLost)
	}
	r = r.WithContext(templates.WithStyleImages(r.Context(), accountStyleImages))
	render(w, r, status, templates.AccountStyle(info.Email, info.WorkspaceName, info.CSRFToken, data))
}

// refusedStyleForm is a style form after a refused save: what was typed,
// with the pictures that are stored rather than any that came with the
// form, since one uploaded with a refused form was not stored and there
// is nothing at its address to show; and the problem, worded for the
// head of the form and for beside its field.
func refusedStyleForm(r *http.Request, form templates.StyleForm, typed *domain.Style, problem error, filesLost bool) templates.StyleForm {
	if typed != nil {
		saved := form.Style
		form.Style = *typed
		form.Style.Header.Banner, form.Style.Header.Logo = saved.Header.Banner, saved.Header.Logo
		form.Style.Thanks.Image = saved.Thanks.Image
	}
	form.FilesLost = filesLost
	form.Error = sayErrorAlone(r, problem)
	var where domain.StyleError
	if errors.As(problem, &where) && where.Part != domain.StyleTheme {
		form.ErrorPart, form.ErrorLink = where.Part, where.Link
		form.Error = say(r, styleErrorPlace(where), uitext.Args{"Position": where.Link, "Problem": sayError(r, where.Err)})
		form.FieldError = sayErrorAlone(r, where.Err)
	}
	return form
}

// accountStyleSave saves the account's style. It changes no published
// survey: each takes the account's style when it is next published
// (ADR-0023). The form is read as the Style tab's is, part by part over
// the style it edits, and its pictures are stored with the style in one
// transaction.
func (s *server) accountStyleSave(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	current, err := s.surveys.WorkspaceStyle(r.Context(), info.WorkspaceID)
	if err != nil {
		s.internalError(w, r, "load workspace style", err)
		return
	}
	style, err := styleFromForm(r, current.Style)
	style, pictures, pictureErr := s.stylePicturesFromForm(r, style)
	if err == nil {
		err = pictureErr
	}
	if err == nil {
		err = current.SetStyle(style)
	}
	if err == nil {
		err = s.surveys.SaveWorkspaceStyle(r.Context(), info.WorkspaceID, info.UserID, current.Style, storedPictures(pictures), s.clock.Now())
		err = pictureLimitPart(err, pictures)
		if err != nil && !isUserError(err) {
			s.internalError(w, r, "save workspace style", err)
			return
		}
	}
	if err != nil {
		if !isUserError(err) {
			s.internalError(w, r, "set workspace style", err)
			return
		}
		s.renderAccountStyle(w, r, &style, err, "", styleFilesChosen(r))
		return
	}
	http.Redirect(w, r, "/account/style?notice=saved", http.StatusSeeOther)
}

// storedPictures are the prepared pictures, as the store takes them.
func storedPictures(pictures []newPicture) []store.NewImage {
	stored := make([]store.NewImage, len(pictures))
	for i, p := range pictures {
		stored[i] = p.stored
	}
	return stored
}

// pictureLimitPart names the part of the style whose picture did not fit
// under a limit of stored pictures, so the problem is shown beside its
// field. Any other error is returned as it is.
func pictureLimitPart(err error, pictures []newPicture) error {
	var full store.ImageLimitError
	if !errors.As(err, &full) {
		return err
	}
	for _, p := range pictures {
		if p.stored.SHA256 == full.SHA256 {
			return domain.StyleError{Part: p.part, Err: err}
		}
	}
	return err
}

// accountStyleImage serves one of the account's pictures to its own
// creator, for the account's style page. Another workspace's are not
// found, and nobody without a session reaches this.
func (s *server) accountStyleImage(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	img, err := s.surveys.WorkspaceImage(r.Context(), info.WorkspaceID, r.PathValue("sha256"))
	if !s.styleImageFound(w, r, err) {
		return
	}
	if styleImageHeaders(w, r, img.SHA256, draftImageCache) {
		return
	}
	writeStyleImage(w, r, img)
}
