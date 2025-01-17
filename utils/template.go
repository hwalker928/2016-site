package utils

import (
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gedex/inflector"

	"github.com/UniversityRadioYork/2016-site/structs"
	"github.com/UniversityRadioYork/myradio-go"
	"github.com/microcosm-cc/bluemonday"
)

// TemplatePrefix is the constant containing the filepath prefix for templates.
const TemplatePrefix = "views"

// BaseTemplates is the array of 'base' templates used in each template render.
var BaseTemplates = []string{
	"partials/header.tmpl",
	"partials/footer.tmpl",
	"elements/navbar.tmpl",
	"partials/base.tmpl",
}

// RenderTemplate renders a 2016site template on the ResponseWriter w.
//
// This function automatically adds in the 2016site base templates, performs
// error handling, and builds a global context.
//
// The PageContext context gives the context for the page to be rendered, sent
// to the template as PageContext.
// The interface{} data gives the data to be sent to the template as PageData.
//
// The string mainTmpl gives the name, relative to views, of the main
// template to render.  The variadic argument addTmpls names any additional
// templates mainTmpl depends on.
//
// RenderTemplate returns any error that occurred when rendering the template.
func RenderTemplate(w http.ResponseWriter, context structs.PageContext, data interface{}, mainTmpl string, addTmpls ...string) error {
	var err error

	td := structs.Globals{
		PageContext: context,
		PageData:    data,
	}

	ownTmpls := append(addTmpls, mainTmpl)
	baseTmpls := append(BaseTemplates, ownTmpls...)

	var tmpls []string
	for _, baseTmpl := range baseTmpls {
		tmpls = append(tmpls, filepath.Join(TemplatePrefix, baseTmpl))
	}

	t := template.New("base.tmpl")
	t.Funcs(template.FuncMap{
		"url":       func(s string) string { return PrefixURL(s, context.URLPrefix) },
		"html":      renderHTML,
		"stripHtml": StripHTML,
		//Takes a splice of show meta and returns the last x elements
		"getLastShowMeta": func(a []myradio.ShowMeta, amount int) []myradio.ShowMeta {
			if len(a) < amount {
				return a
			}
			return a[len(a)-amount:]

		},
		//Takes a splice of seasons and returns the total number of episodes
		"showCount": func(seasons []myradio.Season) int {
			var c = 0
			for _, season := range seasons {
				//Something about JSON being read as a float 64 so needing to convert to an int
				c += int(season.NumEpisodes.Value.(float64))
			}
			return c
		},
		"formatDuration": func(d time.Duration) string {
			days := int64(d.Hours()) / 24
			hours := int64(d.Hours()) % 24
			minutes := int64(d.Minutes()) % 60
			seconds := int64(d.Seconds()) % 60

			segments := []struct {
				name  string
				value int64
			}{
				{"Day", days},
				{"Hour", hours},
				{"Min", minutes},
				{"Sec", seconds},
			}

			parts := []string{}

			for _, s := range segments {
				if s.value == 0 {
					continue
				}
				plural := ""
				if s.value != 1 {
					plural = "s"
				}

				parts = append(parts, fmt.Sprintf("%d %s%s", s.value, s.name, plural))
			}
			return strings.Join(parts, " ")
		},
		"formatTime": func(fmt string, t time.Time) string {
			return t.Format(fmt)
		},
		"now": func() time.Time {
			return time.Now()
		},
		"subTime": func(aRaw, bRaw interface{}) (time.Duration, error) {
			var a, b time.Time
			var err error
			a, err = coerceTime(aRaw)
			if err != nil {
				return 0, err
			}
			b, err = coerceTime(bRaw)
			if err != nil {
				return 0, err
			}
			return a.Sub(b), nil
		},
		"week":   FormatWeekRelative,
		"plural": inflector.Pluralize,
	})
	t, err = t.ParseFiles(tmpls...)
	if err != nil {
		return err
	}

	return t.Execute(w, td)
}

// renderHTML takes some html as a string and returns a template.HTML
//
// Handles plain text gracefully.
func renderHTML(value string) template.HTML {
  sanitizer := bluemonday.UGCPolicy()
  sanitizedString := sanitizer.Sanitize(value)

	return template.HTML(sanitizedString)
}
