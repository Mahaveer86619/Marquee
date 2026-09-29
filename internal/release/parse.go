package release

import (
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/dreulavelle/jhin"
)

// ParseName parses a release name with jhin and normalizes the result.
func ParseName(name string) Parsed {
	r := jhin.Parse(name)
	p := Parsed{
		Title:      r.Title,
		Resolution: strings.ToLower(r.Resolution),
		Quality:    r.Quality,
		Codec:      strings.ToLower(r.Codec),
		Audio:      r.Audio,
		Channels:   r.Channels,
		HDR:        r.HDR,
		BitDepth:   r.BitDepth,
		DualAudio:  r.DualAudio,
		Seasons:    r.Seasons,
		Episodes:   r.Episodes,
		Complete:   r.Complete,
		Group:      r.Group,
		Trash:      r.Trash,
		Repack:     r.Repack || r.Proper,
	}
	if y, err := strconv.Atoi(r.Year); err == nil {
		p.Year = y
	}
	for _, l := range r.Languages {
		p.Languages = appendUnique(p.Languages, ToISO1(l))
	}
	for _, s := range r.Subtitles {
		p.Subtitles = appendUnique(p.Subtitles, ToISO1(s))
	}
	p.Multi = multiPattern.MatchString(name)
	return p
}

var multiPattern = regexp.MustCompile(`(?i)\bmulti\b`)

// Video, subtitle and audio file extensions.
var (
	videoExt    = map[string]bool{".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".webm": true, ".mov": true, ".ts": true, ".ogv": true, ".mpg": true, ".mpeg": true}
	subtitleExt = map[string]bool{".srt": true, ".ass": true, ".ssa": true, ".vtt": true, ".sub": true, ".idx": true, ".sup": true}
	audioExt    = map[string]bool{".mka": true, ".aac": true, ".ac3": true, ".eac3": true, ".dts": true, ".flac": true, ".m4a": true, ".opus": true}
)

// FileKind classifies a file by extension.
func FileKind(name string) string {
	ext := strings.ToLower(path.Ext(name))
	switch {
	case videoExt[ext]:
		return "video"
	case subtitleExt[ext]:
		return "subtitle"
	case audioExt[ext]:
		return "audio"
	}
	return "other"
}

var fileLangPattern = regexp.MustCompile(`(?i)[._ \-\[(]([a-z]{2,3}|english|hindi|spanish|french|german|italian|japanese|korean|chinese|tamil|telugu|russian|portuguese|arabic)(?:[._ \-](?:forced|sdh|cc|hi))?\.[a-z0-9]+$`)

// FileLanguage guesses the language of a subtitle or audio file from a suffix
// such as ".en.srt", "_eng.srt" or ".Hindi.mka". It returns "" when unknown.
func FileLanguage(name string) string {
	m := fileLangPattern.FindStringSubmatch(path.Base(name))
	if m == nil {
		return ""
	}
	code := ToISO1(m[1])
	for _, l := range languageTable {
		if l.one == code {
			return code
		}
	}
	return ""
}

// AudioLanguages lists the audio languages a release offers: advertised in
// the name, or as separate audio files.
func (r Release) AudioLanguages() []string {
	out := append([]string(nil), r.Parsed.Languages...)
	for _, f := range r.Files {
		if f.Kind == "audio" && f.Language != "" {
			out = appendUnique(out, f.Language)
		}
	}
	return out
}

// SubtitleLanguages lists the subtitle languages a release offers.
func (r Release) SubtitleLanguages() []string {
	out := append([]string(nil), r.Parsed.Subtitles...)
	for _, f := range r.Files {
		if f.Kind == "subtitle" && f.Language != "" {
			out = appendUnique(out, f.Language)
		}
	}
	return out
}

func appendUnique(list []string, v string) []string {
	if v == "" {
		return list
	}
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}
