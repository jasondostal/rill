package auth

import (
	"html/template"
	"net/http"

	rilllog "github.com/jasondostal/rill/internal/log"
)

// consentCookieName ties a consent page to the browser it was shown in.
const consentCookieName = "rill_oauth_consent"

const oauthPageStyle = `<style>
:root{color-scheme:dark}
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0f1115;color:#e6e8ee;font:16px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif}
main{width:min(30rem,calc(100vw - 2rem));background:#171a21;border:1px solid #2a2f3a;border-radius:12px;padding:1.75rem}
h1{font-size:1.2rem;margin:0 0 1rem}
p{margin:.6rem 0}
.who{font-weight:600;color:#fff;overflow-wrap:anywhere}
.dest{display:block;margin:.4rem 0 1rem;padding:.6rem .75rem;background:#0f1115;border:1px solid #2a2f3a;border-radius:8px;font:600 1rem ui-monospace,SFMono-Regular,Menlo,monospace;overflow-wrap:anywhere}
ul{margin:.3rem 0 1rem;padding-left:1.2rem}
.warn{color:#f0c36d;font-size:.9rem}
.row{display:flex;gap:.75rem;margin-top:1.4rem}
button{flex:1;padding:.7rem 1rem;border-radius:8px;border:1px solid #2a2f3a;background:#222733;color:#e6e8ee;font:inherit;cursor:pointer}
button.go{background:#3b6cf6;border-color:#3b6cf6;color:#fff;font-weight:600}
a{color:#8ab0ff}
</style>`

var consentPageTmpl = template.Must(template.New("consent").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>Authorize access to Rill</title>` + oauthPageStyle + `</head><body><main>
<h1>Authorize access to Rill?</h1>
<p><span class="who">{{if .ClientName}}{{.ClientName}}{{else}}An unnamed application{{end}}</span> is asking to connect to this Rill memory.</p>
<p>If you approve, access will be handed to:</p>
<span class="dest">{{.RedirectHost}}</span>
<p>It will be able to:</p>
<ul>{{range .Scopes}}<li>{{if eq . "read"}}Read your memories, entities and documents{{else if eq . "write"}}Add, change and remove memories{{else if eq . "admin"}}Administer the server: tokens, settings, permanent deletes{{else}}{{.}}{{end}}</li>{{end}}</ul>
<p class="warn">The name above is whatever the application chose to call itself. Only approve if you started this yourself and you recognise the destination.</p>
<form method="post" action="/oauth/authorize/confirm">
<input type="hidden" name="consent_token" value="{{.ConsentToken}}">
<div class="row">
<button type="submit" name="action" value="deny">Deny</button>
<button type="submit" name="action" value="approve" class="go">Approve</button>
</div></form>
</main></body></html>`))

var continuePageTmpl = template.Must(template.New("continue").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="referrer" content="no-referrer">
<meta http-equiv="refresh" content="0;url={{.URL}}">
<title>Rill</title>` + oauthPageStyle + `</head><body><main>
<p>{{.Message}}</p>
<p><a href="{{.URL}}">Continue</a> if nothing happens.</p>
</main></body></html>`))

func renderOAuthPage(w http.ResponseWriter, status int, t *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := t.Execute(w, data); err != nil {
		rilllog.Logger().Error("oauth: render page failed", "error", err)
	}
}
