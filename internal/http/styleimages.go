package http

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/semaphore"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/styleimage"
	"github.com/TryEarful/earful/web/templates"
)

// The pictures of a survey's style (ADR-0018): a logo, a banner and a
// picture for the thanks page, uploaded on the Style tab, stored beside the survey and served from
// Earful's own origin, so a respondent's page stays first party
// (ADR-0006).

// maxStyleRequestBytes caps the Style tab's form: its pictures at their
// own limits (styleimage.Banner, styleimage.Logo, styleimage.Thanks),
// with room for the form's fields and the multipart framing. A file over its own
// limit and under this one is refused with a message beside its field.
const maxStyleRequestBytes = 6 << 20

// The form fields of the Style tab's pictures.
const (
	bannerField       = "banner"
	bannerRemoveField = "banner_remove"
	logoField         = "logo"
	logoRemoveField   = "logo_remove"
	logoAltField      = "logo_alt"
	// The thanks page's picture: the choice, the creator's own file, the
	// box that removes it, and its alternative text.
	thanksPictureField     = "thanks_picture"
	thanksImageField       = "thanks_image"
	thanksImageRemoveField = "thanks_image_remove"
	thanksAltField         = "thanks_alt"
)

// decodeBudget is the memory all the pictures being decoded at once may
// take together. A decoded picture is far larger than its file, and one
// may take up to styleimage.MaxDecodeBytes, so each decode waits until
// what it will take fits beside the others rather than being left to
// however many forms arrive together. It is sized for the service's
// instance (deploy/opentofu/modules/run-service): one picture at the
// largest, or several small ones, beside everything else the instance
// holds.
const decodeBudget = 160 << 20

// decodeHeadroom is taken beside each decode's estimate, for what
// preparing a picture holds besides its decoding: the scaled picture and
// the encoder's buffers. Measured, these add up to ten megabytes or less.
const decodeHeadroom = 16 << 20

// The largest decode, with its headroom, must fit the budget on its own,
// or it would wait for good: this fails to compile if it does not.
const _ = uint64(decodeBudget - styleimage.MaxDecodeBytes - decodeHeadroom)

// decoding holds what the decodes under way have taken of decodeBudget.
var decoding = semaphore.NewWeighted(decodeBudget)

// styleSavesAtOnce bounds the Style forms handled at once (Deps.StyleSaves).
// A form is parsed whole into memory, up to maxStyleRequestBytes, and its
// files are copied as they are read, all before its pictures wait for
// room in decodeBudget; nothing else bounds how many wait. Four is two
// forms decoding at the largest and two read and waiting their turn: a
// fifth would only hold one more body in memory while it waits, and a
// creator saves a style a handful of times in a sitting.
const styleSavesAtOnce = 4

// styleBodyTime is how long a Style form's body may take to arrive
// (Deps.StyleBodyTime). The form is at most maxStyleRequestBytes, which
// a phone sending at half a megabit a second delivers in about a minute
// and a half; past two minutes a body is not coming, and a form that is
// sent a byte at a time would otherwise hold its place among
// styleSavesAtOnce for as long as the sender liked.
const styleBodyTime = 2 * time.Minute

// styleSavers is the people with a Style form being handled. Each may
// have one at a time, so that one person cannot take every place among
// styleSavesAtOnce; nobody saves two styles at the same moment. An entry
// lasts as long as its form, so the set is never larger than the forms
// being handled.
type styleSavers struct {
	mu     sync.Mutex
	saving map[uuid.UUID]struct{}
}

// begin takes the person's place, or reports that they already have a
// form being handled.
func (v *styleSavers) begin(user uuid.UUID) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, ok := v.saving[user]; ok {
		return false
	}
	if v.saving == nil {
		v.saving = map[uuid.UUID]struct{}{}
	}
	v.saving[user] = struct{}{}
	return true
}

// end gives the person's place back.
func (v *styleSavers) end(user uuid.UUID) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.saving, user)
}

// fewStyleSavesAtOnce refuses a Style form at once, before its body is
// read, while its sender has another being handled or styleSavesAtOnce
// are, rather than let it queue with its body in memory. The places are
// given back however the form ends, and its body must arrive within
// styleBodyTime, so a body that never comes cannot keep one.
func (s *server) fewStyleSavesAtOnce(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, _ := authFrom(r.Context())
		if !s.styleSavers.begin(info.UserID) {
			s.styleBusy(w, r)
			return
		}
		defer s.styleSavers.end(info.UserID)
		if !s.styleSaves.TryAcquire(1) {
			s.styleBusy(w, r)
			return
		}
		defer s.styleSaves.Release(1)
		s.styleBodyInTime(next).ServeHTTP(w, r)
	})
}

// styleBodyInTime reads a Style form's body under a deadline and lifts
// the deadline once the body is in. Left in place, it would end the
// request's context while its pictures are prepared and stored, since
// the server takes a read that times out after the body for a client
// gone away.
func (s *server) styleBodyInTime(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A network deadline, so the wall clock, not s.clock.
		s.setReadDeadline(w, time.Now().Add(s.styleBodyTime))
		s.readUploads(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// A form that is not multipart is read here, under the
			// deadline, rather than later by the CSRF check without one.
			if r.Form == nil {
				if err := r.ParseForm(); err != nil {
					http.Error(w, "bad request", http.StatusBadRequest)
					return
				}
			}
			s.setReadDeadline(w, time.Time{})
			next.ServeHTTP(w, r)
		})).ServeHTTP(w, r)
	})
}

// setReadDeadline sets or, with the zero time, lifts the deadline for
// reading the request's body. A writer that cannot, such as a test's
// recorder, goes without.
func (s *server) setReadDeadline(w http.ResponseWriter, at time.Time) {
	err := http.NewResponseController(w).SetReadDeadline(at)
	if err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.logger.Warn("style form read deadline", "error", err)
	}
}

// styleBusy says a Style form was not saved because others are being,
// with a way back to the Style tab to save it again.
func (s *server) styleBusy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After", "10")
	title, body := say(r, "style.busy.title"), say(r, "style.busy.body")
	id, err := uuid.Parse(r.PathValue("surveyID"))
	if err != nil {
		render(w, r, http.StatusServiceUnavailable, templates.ErrorPage(title, body))
		return
	}
	render(w, r, http.StatusServiceUnavailable, templates.ErrorPageBack(
		title, body, "/surveys/"+id.String()+"/style", say(r, "style.busy.back")))
}

// uploadTooLarge is a picture over its slot's byte limit, with the limit
// for the message.
type uploadTooLarge struct{ megabytes int64 }

func (e uploadTooLarge) Error() string {
	return "keep the file under " + strconv.FormatInt(e.megabytes, 10) + " MB"
}

// newPicture is a picture uploaded with the Style form, prepared and not
// yet stored, and the part of the style it is for.
type newPicture struct {
	part   domain.StylePart
	stored store.NewImage
}

// draftStyleImages is where a creator's pages fetch a survey's pictures
// from: the Style tab and the preview show the draft's, which the public
// address does not serve until a version is published with them.
func draftStyleImages(surveyID uuid.UUID) string {
	return "/surveys/" + surveyID.String() + "/style-image/"
}

// stylePicturesFromForm lays the form's picture fields over a style: a
// box ticked removes a picture, a file chosen replaces it, and a field
// left alone keeps what is there. The alternative texts are read with
// them. A form that does not carry a picture's fields at all, such as one
// drawn before they existed, changes nothing of that picture. A file
// chosen for the thanks page makes it the page's picture, whatever choice
// was left ticked: choosing a file says which picture is wanted.
//
// The pictures are returned prepared and not stored: the caller stores
// them once the whole style is accepted. The style is returned as typed
// whether or not it is refused, with the first problem found.
func (s *server) stylePicturesFromForm(r *http.Request, style domain.Style) (domain.Style, []newPicture, error) {
	var pictures []newPicture
	var problem error
	read := func(field, removeField string, part domain.StylePart, slot styleimage.Slot, current domain.StyleImage) domain.StyleImage {
		if r.PostFormValue(removeField) != "" {
			current = domain.StyleImage{}
		}
		upload, err := uploadedFile(r, field)
		if err != nil || upload == nil {
			if err != nil && problem == nil {
				problem = domain.StyleError{Part: part, Err: styleimage.ErrUnreadable}
			}
			return current
		}
		prepared, err := preparePicture(r.Context(), upload, slot)
		if err != nil {
			if errors.Is(err, styleimage.ErrTooLarge) {
				err = uploadTooLarge{megabytes: megabytes(slot.MaxUpload)}
			}
			if problem == nil {
				problem = domain.StyleError{Part: part, Err: err}
			}
			return current
		}
		pictures = append(pictures, newPicture{part: part, stored: store.NewImage{
			SHA256: prepared.SHA256, ContentType: prepared.ContentType,
			Width: prepared.Width, Height: prepared.Height, Bytes: prepared.Bytes,
		}})
		return domain.StyleImage{SHA256: prepared.SHA256, Width: prepared.Width, Height: prepared.Height}
	}
	if r.PostForm.Has(logoAltField) {
		style.Header.Banner = read(bannerField, bannerRemoveField, domain.StyleBanner, styleimage.Banner, style.Header.Banner)
		style.Header.Logo = read(logoField, logoRemoveField, domain.StyleLogo, styleimage.Logo, style.Header.Logo)
		style.Header = style.Header.WithLogoAlt(r.PostFormValue(logoAltField))
	}
	if r.PostForm.Has(thanksAltField) {
		before := len(pictures)
		style.Thanks.Image = read(thanksImageField, thanksImageRemoveField, domain.StyleThanksImage, styleimage.Thanks, style.Thanks.Image)
		if len(pictures) > before {
			style.Thanks.Picture = domain.ThanksImage
		}
		style.Thanks = style.Thanks.WithAlt(r.PostFormValue(thanksAltField))
	}
	return style, pictures, problem
}

// preparePicture runs styleimage.Prepare once there is room in
// decodeBudget for what the picture's decoding will take, with its
// headroom, or gives up when the request does. A picture refused on its
// header is refused without waiting.
func preparePicture(ctx context.Context, upload []byte, slot styleimage.Slot) (styleimage.Image, error) {
	cost, err := styleimage.Cost(upload, slot)
	if err != nil {
		return styleimage.Image{}, err
	}
	cost += decodeHeadroom
	if err := decoding.Acquire(ctx, cost); err != nil {
		return styleimage.Image{}, err
	}
	defer decoding.Release(cost)
	return styleimage.Prepare(upload, slot)
}

// uploadedFile is the file posted in one field, read whole, or nil where
// none was chosen: a file input left empty still posts a part with no
// name and no bytes.
func uploadedFile(r *http.Request, field string) ([]byte, error) {
	if r.MultipartForm == nil {
		return nil, nil
	}
	for _, header := range r.MultipartForm.File[field] {
		if header.Filename == "" && header.Size == 0 {
			continue
		}
		return readUpload(header)
	}
	return nil, nil
}

// styleFilesChosen reports whether the Style form came with a file.
func styleFilesChosen(r *http.Request) bool {
	for _, field := range []string{bannerField, logoField, thanksImageField} {
		if data, err := uploadedFile(r, field); err != nil || data != nil {
			return true
		}
	}
	return false
}

// styleImage serves a picture that a published version shows, to
// anybody. A hash only a draft refers to, and a deleted survey's, are
// not found. Fetching a picture records nothing: it is not an open, and
// it does not count against anybody (ADR-0003).
//
// A browser asking whether the picture it kept is current, and a HEAD,
// are answered from the picture's row without its bytes: whether it may
// be fetched is still asked each time.
func (s *server) styleImage(w http.ResponseWriter, r *http.Request) {
	sha := r.PathValue("sha256")
	if r.Method == http.MethodHead || r.Header.Get("If-None-Match") != "" {
		meta, err := s.surveys.PublishedImageMeta(r.Context(), sha)
		if !s.styleImageFound(w, r, err) {
			return
		}
		if styleImageHeaders(w, r, sha, publicImageCache) {
			return
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", meta.ContentType)
			w.Header().Set("Content-Length", strconv.Itoa(meta.Size))
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	img, err := s.surveys.PublishedImage(r.Context(), sha)
	if !s.styleImageFound(w, r, err) {
		return
	}
	if styleImageHeaders(w, r, img.SHA256, publicImageCache) {
		return
	}
	writeStyleImage(w, r, img)
}

// surveyStyleImage serves a picture stored for a survey to its creator,
// for the Style tab and the preview. A survey in another workspace has
// none.
func (s *server) surveyStyleImage(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	surveyID, err := uuid.Parse(r.PathValue("surveyID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	img, err := s.surveys.DraftImage(r.Context(), info.WorkspaceID, surveyID, r.PathValue("sha256"))
	if !s.styleImageFound(w, r, err) {
		return
	}
	if styleImageHeaders(w, r, img.SHA256, draftImageCache) {
		return
	}
	writeStyleImage(w, r, img)
}

// How long a browser may keep a picture. A published picture's address
// is the hash of its bytes, so the bytes at an address never change and
// a browser may keep them for good; that replaces the no-store that
// SecurityHeaders gives every page. A picture fetched for a creator's own
// pages may be one no version shows, so a browser keeps it only to ask
// again whether it is current, and a computer that others use does not
// keep a draft's pictures after its creator has signed out.
const (
	publicImageCache = "public, max-age=31536000, immutable"
	draftImageCache  = "private, no-cache"
)

// styleImageFound reports whether a picture was found, and answers the
// request where it was not.
func (s *server) styleImageFound(w http.ResponseWriter, r *http.Request, err error) bool {
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return false
	}
	if err != nil {
		s.internalError(w, r, "load style image", err)
		return false
	}
	return true
}

// styleImageHeaders sets a picture's caching headers, and answers a
// browser whose kept copy is current; it reports whether it answered.
func styleImageHeaders(w http.ResponseWriter, r *http.Request, sha, cache string) bool {
	etag := `"` + sha + `"`
	w.Header().Set("Cache-Control", cache)
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
}

// writeStyleImage writes a stored picture. The type sent is the stored
// one, which with nosniff means the bytes are drawn as a picture or not
// at all.
func writeStyleImage(w http.ResponseWriter, r *http.Request, img store.Image) {
	w.Header().Set("Content-Type", img.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(img.Bytes)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		w.Write(img.Bytes) //nolint:errcheck // the reader went away
	}
}

// --- the theme sheet's pictures ---------------------------------------------

// The theme sheet's sample survey is nobody's, so it has no stored
// pictures. Its logo and banner are drawn here, from plain shapes, and
// served at an address of the sheet's own. Development only, as the
// sheet is.
const sheetStyleImages = "/dev/theme-sheet/image/"

var sheetStyle = struct{ banner, logo domain.StyleImage }{
	banner: domain.StyleImage{SHA256: "banner", Width: 1200, Height: 400},
	logo:   domain.StyleImage{SHA256: "logo", Width: 240, Height: 240},
}

func (s *server) themeSheetImage(w http.ResponseWriter, r *http.Request) {
	var img *image.NRGBA
	switch r.PathValue("name") {
	case sheetStyle.banner.SHA256:
		img = sheetBanner(sheetStyle.banner.Width, sheetStyle.banner.Height)
	case sheetStyle.logo.SHA256:
		img = sheetLogo(sheetStyle.logo.Width)
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", styleimage.TypePNG)
	png.Encode(w, img) //nolint:errcheck // the reader went away
}

// sheetBanner is a band of warm colour shading into a cool one, with a
// darker rise along its foot: enough to read as a photograph.
func sheetBanner(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			t := float64(x) / float64(w)
			c := color.NRGBA{uint8(232 - 150*t), uint8(190 - 70*t), uint8(140 + 40*t), 0xff}
			rise := float64(h) * (0.72 - 0.18*t*(1-t)*4)
			if float64(y) > rise {
				c = color.NRGBA{0x2a, 0x4a, 0x55, 0xff}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// sheetLogo is a dark ring with a dot in it on a transparent ground, as
// most logos are dark, which is what the plate behind a logo is for.
func sheetLogo(side int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	centre := float64(side) / 2
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			dx, dy := float64(x)+0.5-centre, float64(y)+0.5-centre
			d2 := dx*dx + dy*dy
			switch {
			case d2 <= centre*centre*0.09:
				img.SetNRGBA(x, y, color.NRGBA{0xe0, 0x9a, 0x2b, 0xff})
			case d2 <= centre*centre*0.72 && d2 >= centre*centre*0.36:
				img.SetNRGBA(x, y, color.NRGBA{0x18, 0x22, 0x30, 0xff})
			}
		}
	}
	return img
}

// withSheetStyleImages gives the theme sheet's style its pictures and
// the context their address.
func withSheetStyleImages(r *http.Request, style domain.Style) (*http.Request, domain.Style) {
	style.Header.Banner, style.Header.Logo = sheetStyle.banner, sheetStyle.logo
	style.Header.LogoAlt = style.Header.Name
	return r.WithContext(templates.WithStyleImages(r.Context(), sheetStyleImages)), style
}
