package pkg

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"text/template"

	"github.com/aarol/reload"
	chroma_html "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/chrishrb/go-grip/defaults"
)

type Server struct {
	parser      *Parser
	theme       string
	boundingBox bool
	host        string
	port        int
	browser     bool
}

func NewServer(host string, port int, theme string, boundingBox bool, browser bool, parser *Parser) *Server {
	return &Server{
		host:        host,
		port:        port,
		theme:       theme,
		boundingBox: boundingBox,
		browser:     browser,
		parser:      parser,
	}
}

func (s *Server) Serve(file string) error {
	directory := path.Dir(file)
	filename := path.Base(file)

	reload := reload.New(directory)
	reload.DebugLog = log.New(io.Discard, "", 0)

	validThemes := map[string]bool{"light": true, "dark": true, "auto": true}
	if !validThemes[s.theme] {
		log.Println("Warning: Unknown theme ", s.theme, ", defaulting to 'auto'")
		s.theme = "auto"
	}

	// Use a new ServeMux for cleaner handling
	mux := http.NewServeMux()

	// Handler for embedded static assets
	mux.Handle("/static/", http.FileServer(http.FS(defaults.StaticFiles)))

	// Handler for the content directory
	contentDir := http.Dir(directory)
	contentFileServer := http.FileServer(contentDir)

	// Regex for markdown files
	regex := regexp.MustCompile(`(?i)\.md$`)

	// Main handler for rendering markdown or serving files
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Check if the URL path looks like a markdown file
		if regex.MatchString(r.URL.Path) {
			// Attempt to read the markdown file
			markdownBytes, err := readToString(contentDir, r.URL.Path)
			if err != nil {
				// If reading fails (e.g., file not found), fall back to the file server.
				// The file server will correctly generate a 404 Not Found error.
				contentFileServer.ServeHTTP(w, r)
				return
			}

			// Successfully read the file, so convert it to HTML
			htmlContent := s.parser.MdToHTML(markdownBytes)

			// Serve the final HTML page using the template
			err = serveTemplate(w, htmlStruct{
				Content:      string(htmlContent),
				Theme:        s.theme,
				BoundingBox:  s.boundingBox,
				CssCodeLight: getCssCode("github"),
				CssCodeDark:  getCssCode("github-dark"),
			})
			if err != nil {
				log.Println("Error serving template:", err)
				http.Error(w, "Could not serve template", http.StatusInternalServerError)
				return
			}
		} else {
			// Not a markdown file, so serve it as a static file from the content directory
			contentFileServer.ServeHTTP(w, r)
		}
	})

	addr := fmt.Sprintf("http://%s:%d/", s.host, s.port)
	if file == "" {
		// If README.md exists then open README.md at beginning
		readme := "README.md"
		if f, err := contentDir.Open(readme); err == nil {
			f.Close()
			addr, _ = url.JoinPath(addr, readme)
		}
	} else {
		addr, _ = url.JoinPath(addr, filename)
	}

	fmt.Printf("🚀 Starting server: %s\n", addr)

	if s.browser {
		err := Open(addr)
		if err != nil {
			fmt.Println("❌ Error opening browser:", err)
		}
	}

	// Wrap the new mux with the reload handler
	handler := reload.Handle(mux)
	return http.ListenAndServe(fmt.Sprintf(":%d", s.port), handler)
}

func readToString(dir http.Dir, filename string) ([]byte, error) {
	f, err := dir.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var buf bytes.Buffer
	_, err = buf.ReadFrom(f)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type htmlStruct struct {
	Content      string
	Theme        string
	BoundingBox  bool
	CssCodeLight string
	CssCodeDark  string
}

func serveTemplate(w http.ResponseWriter, html htmlStruct) error {
	w.Header().Set("Content-Type", "text/html")
	tmpl, err := template.ParseFS(defaults.Templates, "templates/layout.html")
	if err != nil {
		return err
	}
	err = tmpl.Execute(w, html)
	return err
}

func getCssCode(style string) string {
	buf := new(strings.Builder)
	formatter := chroma_html.New(chroma_html.WithClasses(true))
	s := styles.Get(style)
	_ = formatter.WriteCSS(buf, s)
	return buf.String()
}
