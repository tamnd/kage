package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/tamnd/kage/clone"
	"github.com/tamnd/kage/urlx"
)

// cloneFlags holds the parsed flag values for one invocation.
type cloneFlags struct {
	out          string
	reserved     string
	workers      int
	assetWorkers int
	browserPages int
	maxPages     int
	maxDepth     int
	traversal    string
	maxAssetMB   int64
	timeout      time.Duration
	settle       time.Duration
	renderTO     time.Duration
	scroll       bool
	userAgent    string
	subdomains   bool
	scopePrefix  string
	exclude      []string
	noRobots     bool
	noSitemap    bool
	headful      bool
	keepNoscript bool
	chromeBin    string
	controlURL   string
	noResume     bool
	refresh      bool
	force        bool
	quiet        bool
}

func newCloneCmd() *cobra.Command {
	f := &cloneFlags{}
	cmd := &cobra.Command{
		Use:   "clone <url>",
		Short: "Clone a site into a self-contained offline folder",
		Long: "clone fetches the seed URL, follows in-scope links, and writes a browsable\n" +
			"mirror to <out>/<host>/. Every page is rendered in Chrome and stripped of\n" +
			"scripts; CSS, images, and fonts are downloaded and rewritten to local paths.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClone(cmd.Context(), args[0], f)
		},
	}
	fs := cmd.Flags()
	fs.StringVarP(&f.out, "out", "o", clone.DefaultOutDir(), "output root; the mirror lands in <out>/<host>/")
	fs.StringVar(&f.reserved, "reserved", urlx.DefaultReserved, "reserved dir for assets and state")
	fs.IntVar(&f.workers, "workers", 4, "concurrent page render workers")
	fs.IntVar(&f.assetWorkers, "asset-workers", 8, "concurrent asset download workers")
	fs.IntVar(&f.browserPages, "browser-pages", 4, "Chrome page-pool size")
	fs.IntVarP(&f.maxPages, "max-pages", "p", 0, "stop after N pages (0 = unlimited)")
	fs.IntVarP(&f.maxDepth, "max-depth", "d", 0, "link-follow depth cap (0 = unlimited)")
	fs.StringVar(&f.traversal, "traversal", "bfs", "frontier order: bfs or dfs")
	fs.Int64Var(&f.maxAssetMB, "max-asset-mb", 25, "skip assets larger than N MB")
	fs.DurationVar(&f.timeout, "timeout", 30*time.Second, "per-request timeout")
	fs.DurationVar(&f.settle, "settle", 1500*time.Millisecond, "network-idle quiet period before snapshot")
	fs.DurationVar(&f.renderTO, "render-timeout", 30*time.Second, "hard cap per page render")
	fs.BoolVar(&f.scroll, "scroll", false, "auto-scroll each page to trigger lazy loading")
	fs.StringVar(&f.userAgent, "user-agent", clone.DefaultUserAgent, "User-Agent for asset and robots fetches")
	fs.BoolVar(&f.subdomains, "subdomains", false, "treat subdomains of the seed host as in scope")
	fs.StringVar(&f.scopePrefix, "scope-prefix", "", "only crawl pages whose path starts with this prefix")
	fs.StringSliceVar(&f.exclude, "exclude", nil, "path prefixes to skip (repeatable)")
	fs.BoolVar(&f.noRobots, "no-robots", false, "ignore robots.txt (be careful and polite)")
	fs.BoolVar(&f.noSitemap, "no-sitemap", false, "do not seed URLs from sitemap.xml")
	fs.BoolVar(&f.headful, "headful", false, "run Chrome with a visible window (debugging)")
	fs.BoolVar(&f.keepNoscript, "keep-noscript", false, "unwrap <noscript> content instead of dropping it")
	fs.StringVar(&f.chromeBin, "chrome", "", "path to the Chrome/Chromium binary")
	fs.StringVar(&f.controlURL, "control-url", "", "attach to an existing Chrome DevTools endpoint")
	fs.BoolVar(&f.noResume, "no-resume", false, "do not reuse or write resume state")
	fs.BoolVar(&f.refresh, "refresh", false, "re-render every page in place to pull in changed content")
	fs.BoolVarP(&f.force, "force", "f", false, "delete any existing mirror for the host first")
	fs.BoolVarP(&f.quiet, "quiet", "q", false, "suppress per-page progress lines")
	return cmd
}

func runClone(ctx context.Context, arg string, f *cloneFlags) error {
	seed, err := urlx.ParseSeed(arg)
	if err != nil {
		return fmt.Errorf("invalid url %q: %w", arg, err)
	}

	cfg := clone.DefaultConfig()
	cfg.OutDir = f.out
	cfg.Reserved = f.reserved
	cfg.Workers = f.workers
	cfg.AssetWorkers = f.assetWorkers
	cfg.BrowserPages = f.browserPages
	cfg.MaxPages = f.maxPages
	cfg.MaxDepth = f.maxDepth
	cfg.Traversal = f.traversal
	cfg.MaxAssetBytes = f.maxAssetMB << 20
	cfg.Timeout = f.timeout
	cfg.Settle = f.settle
	cfg.RenderTimeout = f.renderTO
	cfg.Scroll = f.scroll
	cfg.UserAgent = f.userAgent
	cfg.IncludeSubdomains = f.subdomains
	cfg.ScopePrefix = f.scopePrefix
	cfg.ExcludePaths = f.exclude
	cfg.RespectRobots = !f.noRobots
	cfg.FollowSitemap = !f.noSitemap
	cfg.Headless = !f.headful
	cfg.KeepNoscript = f.keepNoscript
	cfg.ChromeBin = f.chromeBin
	cfg.ControlURL = f.controlURL
	cfg.Resume = !f.noResume
	cfg.Persist = !f.noResume
	cfg.Refresh = f.refresh
	cfg.Force = f.force

	logf := func(format string, args ...any) {
		if !f.quiet {
			fmt.Fprintln(os.Stderr, styleDim.Render(fmt.Sprintf(format, args...)))
		}
	}

	fmt.Fprintln(os.Stderr, styleTitle.Render("kage")+" cloning "+styleAccent.Render(seed.String()))

	c := clone.New(seed, cfg, logf)

	// Live progress ticker on a second line, refreshed every second.
	tickCtx, stopTicker := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if f.quiet {
			return
		}
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-tickCtx.Done():
				return
			case <-t.C:
				p := c.Snapshot()
				fmt.Fprintf(os.Stderr, "\r%s", progressLine(p))
			}
		}
	}()

	res, runErr := c.Run(ctx)
	stopTicker()
	<-done
	if !f.quiet {
		fmt.Fprint(os.Stderr, "\r\033[K")
	}

	printSummary(res)

	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		return runErr
	}
	if errors.Is(runErr, context.Canceled) {
		fmt.Fprintln(os.Stderr, styleWarn.Render("interrupted; resume state saved (rerun to continue)"))
	}
	return nil
}

// progressLine renders the single-line live counter. "pages" is the count of
// real pages (distinct paths); when a faceted site spawns query-string variants
// they are shown separately so the page number stays easy to read.
func progressLine(p clone.Progress) string {
	if variants := p.Pages - p.PagePaths; variants > 0 {
		return styleDim.Render(fmt.Sprintf("pages %d  variants %d  assets %d  errors %d  skipped %d",
			p.PagePaths, variants, p.Assets, p.PageErrors+p.AssetErrors, p.Skipped))
	}
	return styleDim.Render(fmt.Sprintf("pages %d  assets %d  errors %d  skipped %d",
		p.PagePaths, p.Assets, p.PageErrors+p.AssetErrors, p.Skipped))
}

// printSummary prints the final tally and where the mirror landed.
func printSummary(res clone.Result) {
	fmt.Fprintln(os.Stderr, styleOK.Render("done")+" "+styleTitle.Render(res.OutDir))
	fmt.Fprintf(os.Stderr, "  %s %d   %s %d\n",
		styleAccent.Render("pages"), res.PagePaths,
		styleAccent.Render("assets"), res.Assets)
	if variants := res.Pages - res.PagePaths; variants > 0 {
		fmt.Fprintf(os.Stderr, "  %s %d\n", styleDim.Render("query variants"), variants)
	}
	if res.PagesLinked > 0 {
		fmt.Fprintf(os.Stderr, "  %s %d\n", styleDim.Render("deduped (linked)"), res.PagesLinked)
	}
	if res.PageErrors+res.AssetErrors > 0 {
		fmt.Fprintf(os.Stderr, "  %s %d\n", styleErr.Render("errors"), res.PageErrors+res.AssetErrors)
		printFailures(res)
	}
	if res.Skipped > 0 {
		fmt.Fprintf(os.Stderr, "  %s %d\n", styleWarn.Render("skipped"), res.Skipped)
	}
	fmt.Fprintf(os.Stderr, "  open %s\n", styleAccent.Render("kage serve "+res.OutDir))
}

// printFailures lists what went wrong, grouped reason and URL, so the error
// count is actionable instead of opaque. The list is capped during the crawl;
// when it overflows, say how many more there were.
func printFailures(res clone.Result) {
	total := res.PageErrors + res.AssetErrors
	for _, f := range res.Failures {
		line := fmt.Sprintf("    %s  %s", styleErr.Render(f.Reason), f.URL)
		fmt.Fprintln(os.Stderr, line)
		if f.Referer != "" {
			fmt.Fprintln(os.Stderr, styleDim.Render("      referenced by "+f.Referer))
		}
	}
	if more := total - int64(len(res.Failures)); more > 0 {
		fmt.Fprintln(os.Stderr, styleDim.Render(fmt.Sprintf("    ... and %d more", more)))
	}
}
