package probe

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/kajisho5/assume-nothing/internal/store"
)

// NetTargets are well-known hosts used to see whether the network is open.
var NetTargets = []string{"https://github.com", "https://pypi.org/simple/", "https://registry.npmjs.org/"}

// Net checks outbound network access. Only proxy variable NAMES are reported, never values.
func Net(ctx context.Context) []store.Fact {
	var out []store.Fact
	var proxies []string
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "NO_PROXY", "no_proxy"} {
		if os.Getenv(k) != "" {
			proxies = append(proxies, k)
		}
	}
	if len(proxies) > 0 {
		out = append(out, store.Fact{Key: "net.proxy", Status: store.OK, Value: "set: " + strings.Join(proxies, ", "), How: "environment variable names only"})
	} else {
		out = append(out, store.Fact{Key: "net.proxy", Status: store.Missing, How: "environment variable names only"})
	}
	client := &http.Client{Timeout: 3 * time.Second}
	res := make([]store.Fact, len(NetTargets))
	var wg sync.WaitGroup
	for i, u := range NetTargets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f := store.Fact{Key: "net." + host(u), How: "HTTP HEAD " + u}
			req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
			if err == nil {
				var resp *http.Response
				resp, err = client.Do(req)
				if err == nil {
					resp.Body.Close()
					if resp.StatusCode < 500 && resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusProxyAuthRequired {
						f.Status, f.Value = store.OK, fmt.Sprintf("reachable (HTTP %d)", resp.StatusCode)
					} else {
						f.Status, f.Value = store.Missing, fmt.Sprintf("answered HTTP %d (blocked or failing)", resp.StatusCode)
					}
				}
			}
			if err != nil {
				f.Status, f.Why = store.Missing, "not reachable: "+Clean(err.Error(), 80)
			}
			res[i] = f
		}()
	}
	wg.Wait()
	return append(out, res...)
}

func host(u string) string {
	u = strings.TrimPrefix(u, "https://")
	if i := strings.IndexByte(u, '/'); i >= 0 {
		u = u[:i]
	}
	return u
}

// Tool checks one tool in depth: where it is and its version. Some tools get extra checks.
func Tool(ctx context.Context, name string) []store.Fact {
	if !toolName.MatchString(name) {
		return []store.Fact{{Key: "tool." + Clean(name, 40), Status: store.Unknown, Why: "invalid tool name"}}
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return []store.Fact{{Key: "tool." + name, Status: store.Missing, How: "PATH lookup"}}
	}
	out := []store.Fact{{Key: "tool." + name + ".path", Status: store.OK, Value: Clean(p, 200), How: "PATH lookup"}}
	ver := store.Fact{Key: "tool." + name + ".version", Status: store.Unknown, Why: "no version flag answered"}
	for _, flag := range []string{"--version", "-version", "-V", "version"} {
		if v, err := firstLine(ctx, name, flag); err == nil && v != "" {
			ver = store.Fact{Key: ver.Key, Status: store.OK, Value: v, How: name + " " + flag}
			break
		}
	}
	out = append(out, ver)
	if name == "ffmpeg" {
		out = append(out, listFact(ctx, "tool.ffmpeg.hwaccels", "ffmpeg", "-hide_banner", "-hwaccels"))
	}
	return out
}

func listFact(ctx context.Context, key, name string, args ...string) store.Fact {
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(c, name, args...)
	cmd.WaitDelay = 200 * time.Millisecond
	b, err := cmd.Output()
	how := name + " " + strings.Join(args, " ")
	if err != nil {
		return store.Fact{Key: key, Status: store.Unknown, How: how, Why: Clean(err.Error(), 60)}
	}
	var items []string
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasSuffix(l, ":") {
			continue
		}
		items = append(items, Clean(l, 30))
	}
	if len(items) == 0 {
		return store.Fact{Key: key, Status: store.Missing, How: how}
	}
	return store.Fact{Key: key, Status: store.OK, Value: strings.Join(items, ", "), How: how}
}
