package preset

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/basti-fantasti/bossy/internal/core/domain"
	"github.com/basti-fantasti/bossy/internal/core/services/presets"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/spf13/cobra"
)

// importFetchTimeout bounds an import over HTTP. A catalog is a small JSON
// document, so anything slower than this means the host is unreachable.
const importFetchTimeout = 30 * time.Second

// importAction is the testable core of `preset import`.
type importAction struct {
	source string
	// read is injectable so tests do not reach the network.
	read func(source string) ([]byte, error)
}

func (a importAction) run(svc *presets.Service) ([]string, error) {
	read := a.read
	if read == nil {
		read = readCatalogSource
	}

	raw, err := read(a.source)
	if err != nil {
		return nil, err
	}

	catalog := domain.Catalog{}
	if decodeErr := json.Unmarshal(raw, &catalog); decodeErr != nil {
		return nil, fmt.Errorf("parse catalog from %s: %w", a.source, decodeErr)
	}

	added, replaced, err := svc.ImportLocal(catalog)
	if err != nil {
		return nil, err
	}

	return []string{fmt.Sprintf("✅ imported %d preset(s) from %s: %d new, %d replaced",
		added+replaced, a.source, added, replaced)}, nil
}

// readCatalogSource reads a catalog from a local path or an http(s) URL.
func readCatalogSource(source string) ([]byte, error) {
	parsed, err := url.Parse(source)
	if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		return fetchCatalog(source)
	}

	raw, err := os.ReadFile(source) // #nosec G304 -- path supplied by the user on the command line
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", source, err)
	}
	return raw, nil
}

// fetchCatalog downloads a catalog over HTTP(S).
func fetchCatalog(source string) ([]byte, error) {
	client := &http.Client{Timeout: importFetchTimeout}

	resp, err := client.Get(source) //nolint:noctx // the timeout above bounds the request
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", source, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: %s", source, resp.Status)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", source, err)
	}
	return raw, nil
}

func importCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import <path|url>",
		Short: "Merge a catalog file into your local catalog",
		Long: `Reads a catalog from a file or an http(s) URL and merges its entries into
your local catalog, replacing local entries that share an id.

Use 'bossy preset sync' instead when the catalog lives in a git repository that
should stay tracked.`,
		Args: cobra.ExactArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			lines, err := importAction{source: strings.TrimSpace(args[0])}.run(Service())
			if err != nil {
				msg.Die("%s", err.Error())
				return
			}
			for _, l := range lines {
				msg.Success("%s", l)
			}
		},
	}
}
