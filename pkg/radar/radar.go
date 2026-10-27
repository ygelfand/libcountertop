package radar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/sync/errgroup"

	"github.com/ygelfand/libcountertop/pkg/fetch"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const (
	blueMarble = "https://gibs.earthdata.nasa.gov/wmts/epsg3857/best/" +
		"BlueMarble_ShadedRelief_Bathymetry/default/GoogleMapsCompatible_Level8/%d/%d/%d.jpeg"
	gibsClouds = "https://gibs.earthdata.nasa.gov/wmts/epsg3857/best/" +
		"%s/default/GoogleMapsCompatible_Level6/%d/%d/%d.png"
	iemTiles     = "https://mesonet.agron.iastate.edu/cache/tile.py/1.0.0/ridge::USCOMP-N0Q-%s/%d/%d/%d.png"
	hrrrIndex    = "https://mesonet.agron.iastate.edu/data/gis/images/4326/hrrr/refd_0000.json"
	hrrrTiles    = "https://mesonet.agron.iastate.edu/cache/tile.py/1.0.0/hrrr::REFD-F%04d-%s/%d/%d/%d.png"
	rainIndex    = "https://api.rainviewer.com/public/weather-maps.json"
	rainTiles    = "%s%s/256/%d/%d/%d/2/1_1.png"
	history      = 2 * time.Hour
	frameStep    = 10 * time.Minute
	frames       = int(history/frameStep) + 1
	nwsLag       = 5 * time.Minute
	ahead        = 2 * time.Hour
	forecastStep = 15 * time.Minute
	hrrrReach    = 18 * time.Hour
	fresh        = 5 * time.Minute
	baseFresh    = 30 * time.Minute
	buildWait    = 90 * time.Second
	retryWait    = 30 * time.Second
	framesAtOnce = 2
	cloudFresh   = 10 * time.Minute
	cloudShrink  = 4
	parallel     = 6
	radarAlpha   = 0xd8
	baseDim      = 184
)

type Frame struct {
	At    time.Time
	Image *image.NRGBA
}

type Map struct {
	View   View
	Frames []Frame
	Credit string
	Built  time.Time
	Home   *[2]float64
	Miles  int
	Now    time.Time
	Times  []time.Time

	asked Request
}

type shot struct {
	at   time.Time
	most int
	life lifetime
	url  func(z, x, y int) string
}

type Request struct {
	Size   image.Point
	Miles  int
	Center *[2]float64
}

type Options struct {
	Home     func(context.Context) (lat, lon float64, err error)
	Client   *http.Client
	Agent    string
	Settings storage.Section[schema.Weather]
	Redraw   func()
	CacheDir string
}

type Radar struct {
	o     Options
	tiles *tiles

	mu       sync.Mutex
	have     Map
	haveGen  int
	asked    Request
	problem  error
	failed   time.Time
	building bool
	gen      int
	cancel   context.CancelFunc
	base     *image.RGBA
	baseView View
	baseAt   time.Time
}

func New(o Options) *Radar {
	if o.Client == nil {
		o.Client = fetch.Client(20 * time.Second)
	}
	return &Radar{o: o, tiles: &tiles{
		client: o.Client,
		agent:  o.Agent,
		limit:  parallel,
		cache:  newCache(o.CacheDir),
	}}
}

func (r *Radar) Get(req Request) (Map, error, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	matches := same(r.have.asked, req) && len(r.have.Frames) > 0
	waiting := r.problem != nil && same(r.asked, req) && time.Since(r.failed) < retryWait
	if r.building && !same(r.asked, req) {
		r.cancel()
		r.building = false
	}
	if !r.building && !waiting && (!matches || time.Since(r.have.Built) > fresh) {
		r.gen++
		ctx, cancel := context.WithTimeout(context.Background(), buildWait)
		r.building, r.asked, r.cancel = true, req, cancel
		go r.build(ctx, cancel, r.gen, req)
	}
	return r.have, r.problem, matches && (!r.building || r.haveGen != r.gen)
}

func same(a, b Request) bool {
	if a.Size != b.Size || a.Miles != b.Miles || (a.Center == nil) != (b.Center == nil) {
		return false
	}
	return a.Center == nil || *a.Center == *b.Center
}

func (r *Radar) build(ctx context.Context, cancel context.CancelFunc, gen int, req Request) {
	defer cancel()
	m, err := r.compose(ctx, req, func(partial Map) {
		r.mu.Lock()
		refreshing := same(r.have.asked, req) && r.haveGen != gen
		current := gen == r.gen && !refreshing
		if current {
			r.have, r.haveGen = partial, gen
		}
		r.mu.Unlock()
		if current && r.o.Redraw != nil {
			r.o.Redraw()
		}
	})
	r.mu.Lock()
	if gen != r.gen {
		r.mu.Unlock()
		return
	}
	if err != nil {
		slog.Warn("building the radar map", "err", err)
	}
	r.building, r.problem = false, err
	if err != nil {
		r.failed = time.Now()
	} else {
		r.have, r.haveGen = m, gen
	}
	r.mu.Unlock()
	if r.o.Redraw != nil {
		r.o.Redraw()
	}
}

func (r *Radar) compose(ctx context.Context, req Request, publish func(Map)) (Map, error) {
	began := time.Now()
	fetched, reused := r.tiles.fetched.Load(), r.tiles.reused.Load()
	var home *[2]float64
	hlat, hlon, err := r.o.Home(ctx)
	if err == nil {
		home = &[2]float64{hlat, hlon}
	} else if req.Center == nil {
		return Map{}, err
	}
	lat, lon := hlat, hlon
	if req.Center != nil {
		lat, lon = req.Center[0], req.Center[1]
	}
	v := View{Lat: lat, Lon: lon, Zoom: zoomFor(lat, req.Miles, req.Size.X), Size: req.Size}
	observed, forecast, credit, err := r.radar(ctx, v)
	if err != nil {
		return Map{}, err
	}
	if len(observed) == 0 {
		return Map{}, errors.New("radar: no frames")
	}
	m := Map{
		View:   v,
		Credit: credit,
		Built:  time.Now(),
		Home:   home,
		Miles:  req.Miles,
		Now:    observed[len(observed)-1].at,
		asked:  req,
	}
	sketch := func(img *image.NRGBA) {
		partial := m
		partial.Frames = []Frame{{At: m.Now, Image: img}}
		publish(partial)
	}
	shots := append(observed, forecast...)
	for _, s := range shots {
		m.Times = append(m.Times, s.at)
	}
	newest := len(observed) - 1
	frames := make([]*Frame, len(shots))
	var (
		mu       sync.Mutex
		ready    sync.WaitGroup
		base     *image.RGBA
		firstErr error
		based    time.Duration
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(framesAtOnce)
	leadCtx, stop := context.WithCancel(gctx)
	defer stop()
	var lead errgroup.Group
	ready.Add(1)
	lead.Go(func() error {
		defer ready.Done()
		b, layer, err := r.first(leadCtx, v, shots[newest], sketch)
		based = time.Since(began)
		if err != nil {
			firstErr = err
			stop()
			return err
		}
		base = b
		if layer == nil {
			sketch(opaque(base))
			return nil
		}
		mu.Lock()
		frames[newest] = &Frame{At: shots[newest].at, Image: overlay(base, layer)}
		mu.Unlock()
		return nil
	})
	for _, i := range fetchOrder(len(observed), len(forecast))[1:] {
		s := shots[i]
		g.Go(func() error {
			layer, err := r.tiles.layer(leadCtx, v, s.most, s.life, s.url, nil)
			if err != nil {
				if leadCtx.Err() != nil {
					return leadCtx.Err()
				}
				slog.Debug("radar frame skipped", "at", s.at, "err", err)
				return nil
			}
			ready.Wait()
			if firstErr != nil {
				return firstErr
			}
			f := &Frame{At: s.at, Image: overlay(base, layer)}
			mu.Lock()
			frames[i] = f
			partial := m
			partial.Frames = done(frames)
			mu.Unlock()
			publish(partial)
			return nil
		})
	}
	leadErr := lead.Wait()
	if err := g.Wait(); err != nil {
		return Map{}, err
	}
	if leadErr != nil {
		return Map{}, leadErr
	}
	m.Frames = done(frames)
	if len(m.Frames) == 0 {
		return Map{}, errors.New("radar: no frames")
	}
	slog.Debug("radar map built",
		"took", time.Since(began),
		"base", based,
		"frames", len(m.Frames),
		"zoom", v.Zoom,
		"fetched", r.tiles.fetched.Load()-fetched,
		"reused", r.tiles.reused.Load()-reused)
	return m, nil
}

func (r *Radar) first(
	ctx context.Context,
	v View,
	s shot,
	sketch func(*image.NRGBA),
) (base, layer *image.RGBA, err error) {
	var (
		mu         sync.Mutex
		seenBase   *image.RGBA
		seenRadar  *image.RGBA
		radarError error
	)
	show := func(b, l *image.RGBA) {
		mu.Lock()
		defer mu.Unlock()
		if b != nil {
			seenBase = b
		}
		if l != nil {
			seenRadar = l
		}
		switch {
		case seenBase == nil:
		case seenRadar == nil:
			sketch(opaque(seenBase))
		default:
			sketch(overlay(seenBase, seenRadar))
		}
	}
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		b, err := r.baseFor(gctx, v, func(b *image.RGBA) { show(b, nil) })
		if err != nil {
			return err
		}
		base = b
		show(b, nil)
		return nil
	})
	g.Go(func() error {
		l, err := r.tiles.layer(
			gctx,
			v,
			s.most,
			s.life,
			s.url,
			func(l *image.RGBA) { show(nil, l) },
		)
		if err != nil {
			radarError = err
			return nil
		}
		layer = l
		show(nil, l)
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}
	if radarError != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		slog.Debug("radar frame skipped", "at", s.at, "err", radarError)
	}
	return base, layer, nil
}

func fetchOrder(observed, forecast int) []int {
	order := make([]int, 0, observed+forecast)
	order = append(order, observed-1)
	for i := range forecast {
		order = append(order, observed+i)
	}
	for i := observed - 2; i >= 0; i-- {
		order = append(order, i)
	}
	return order
}

func overlay(base, layer *image.RGBA) *image.NRGBA {
	pix := make([]uint8, len(base.Pix))
	copy(pix, base.Pix)
	for i := 0; i+3 < len(pix) && i+3 < len(layer.Pix); i += 4 {
		a := uint32(layer.Pix[i+3])
		if a == 0 {
			continue
		}
		a = a * radarAlpha / 255
		for c := range 3 {
			src := uint32(layer.Pix[i+c]) * radarAlpha / 255
			pix[i+c] = uint8(src + uint32(pix[i+c])*(255-a)/255)
		}
	}
	return &image.NRGBA{Pix: pix, Stride: base.Stride, Rect: base.Rect}
}

func opaque(img *image.RGBA) *image.NRGBA {
	return &image.NRGBA{Pix: img.Pix, Stride: img.Stride, Rect: img.Rect}
}

func done(frames []*Frame) []Frame {
	out := make([]Frame, 0, len(frames))
	for _, f := range frames {
		if f != nil {
			out = append(out, *f)
		}
	}
	return out
}

func (r *Radar) baseFor(
	ctx context.Context,
	v View,
	progress func(*image.RGBA),
) (*image.RGBA, error) {
	r.mu.Lock()
	cached, at, kept := r.base, r.baseAt, r.baseView
	r.mu.Unlock()
	if cached != nil && kept == v && time.Since(at) < baseFresh {
		return cached, nil
	}
	img, err := r.tiles.layer(ctx, v, 8, lifetime{keep: true}, func(z, x, y int) string {
		return fmt.Sprintf(blueMarble, z, y, x)
	}, func(b *image.RGBA) {
		if progress != nil {
			dim(b, baseDim)
			progress(b)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("radar: the base map: %w", err)
	}
	dim(img, baseDim)
	if progress != nil {
		progress(&image.RGBA{Pix: slices.Clone(img.Pix), Stride: img.Stride, Rect: img.Rect})
	}
	small := v
	small.Size = image.Pt(max(1, v.Size.X/cloudShrink), max(1, v.Size.Y/cloudShrink))
	small.Zoom = v.Zoom - math.Log2(cloudShrink)
	clouds, err := r.tiles.layer(
		ctx,
		small,
		6,
		lifetime{until: time.Now().Add(cloudFresh)},
		func(z, x, y int) string {
			return fmt.Sprintf(gibsClouds, satellite(v.Lon), z, y, x)
		},
		nil,
	)
	if err != nil {
		slog.Warn("loading satellite clouds", "err", err)
	} else {
		soften(clouds, int(math.Exp2(small.Zoom-6)*0.7))
		full := image.NewRGBA(img.Rect)
		xdraw.ApproxBiLinear.Scale(full, full.Rect, clouds, clouds.Rect, draw.Src, nil)
		overlayClouds(img, full)
	}
	r.mu.Lock()
	r.base, r.baseAt, r.baseView = img, time.Now(), v
	r.mu.Unlock()
	return img, nil
}

func satellite(lon float64) string {
	if lon < -105 || lon > 150 {
		return "GOES-West_ABI_Band13_Clean_Infrared"
	}
	return "GOES-East_ABI_Band13_Clean_Infrared"
}

func dim(img *image.RGBA, k uint32) {
	var lut [256]uint8
	for v := range lut {
		lut[v] = uint8(uint32(v) * k / 255)
	}
	for i := 0; i+3 < len(img.Pix); i += 4 {
		img.Pix[i] = lut[img.Pix[i]]
		img.Pix[i+1] = lut[img.Pix[i+1]]
		img.Pix[i+2] = lut[img.Pix[i+2]]
	}
}

func soften(img *image.RGBA, radius int) {
	if radius < 1 {
		return
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	tmp := make([]uint8, len(img.Pix))
	pass := func(src, dst []uint8, along, across, step, stride int) {
		for a := range across {
			for c := range 4 {
				sum, n := 0, 0
				for i := -radius; i <= radius && i < along; i++ {
					if i >= 0 {
						sum += int(src[a*stride+i*step+c])
						n++
					}
				}
				for i := range along {
					dst[a*stride+i*step+c] = uint8(sum / max(n, 1))
					if out := i - radius; out >= 0 {
						sum -= int(src[a*stride+out*step+c])
						n--
					}
					if in := i + radius + 1; in < along {
						sum += int(src[a*stride+in*step+c])
						n++
					}
				}
			}
		}
	}
	for range 2 {
		pass(img.Pix, tmp, w, h, 4, img.Stride)
		pass(tmp, img.Pix, h, w, img.Stride, 4)
	}
}

func overlayClouds(dst, clouds *image.RGBA) {
	for i := 0; i+3 < len(dst.Pix) && i+3 < len(clouds.Pix); i += 4 {
		alpha := uint32(clouds.Pix[i+3])
		if alpha == 0 {
			continue
		}
		lum := (77*uint32(clouds.Pix[i]) + 150*uint32(clouds.Pix[i+1]) + 29*uint32(clouds.Pix[i+2])) >> 8
		lum = lum * 255 / alpha
		if lum <= 89 {
			continue
		}
		t := min((lum-89)*255/166, 191)
		a := t * alpha / 255
		for c := range 3 {
			dst.Pix[i+c] = uint8((uint32(dst.Pix[i+c])*(255-a) + 235*a) / 255)
		}
	}
}

func (r *Radar) source(v View) schema.RadarSource {
	s := r.o.Settings.Read().RadarSource
	if s == schema.RadarNWS || s == schema.RadarRainViewer {
		return s
	}
	if lower48(v.Lat, v.Lon) {
		return schema.RadarNWS
	}
	return schema.RadarRainViewer
}

func (r *Radar) radar(
	ctx context.Context,
	v View,
) (observed, forecast []shot, credit string, err error) {
	if r.source(v) != schema.RadarNWS {
		observed, credit, err = r.rainviewer(ctx, coarse(v, 7))
		return observed, nil, credit, err
	}
	observed = nws(coarse(v, 10))
	forecast, err = r.hrrr(ctx, coarse(v, 7), observed[len(observed)-1].at)
	if err != nil {
		slog.Warn("loading the radar forecast", "err", err)
	}
	return observed, forecast, "nws", nil
}

func (r *Radar) hrrr(ctx context.Context, most int, after time.Time) ([]shot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hrrrIndex, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", r.o.Agent)
	resp, err := r.o.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("radar: HRRR: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("radar: HRRR: %s", resp.Status)
	}
	var index struct {
		Init time.Time `json:"model_init_utc"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&index); err != nil {
		return nil, fmt.Errorf("radar: HRRR index: %w", err)
	}
	return forecastShots(index.Init, after, most), nil
}

func forecastShots(run, after time.Time, most int) []shot {
	if run.IsZero() {
		return nil
	}
	stamp := run.UTC().Format("200601021504")
	life := lifetime{until: time.Now().Add(ahead + history)}
	var out []shot
	for lead := forecastStep; lead <= hrrrReach; lead += forecastStep {
		at := run.Add(lead)
		if !at.After(after) {
			continue
		}
		if at.After(after.Add(ahead)) {
			break
		}
		minutes := int(lead / time.Minute)
		out = append(out, shot{
			at:   at,
			most: most,
			life: life,
			url: func(z, x, y int) string {
				return fmt.Sprintf(hrrrTiles, minutes, stamp, z, x, y)
			},
		})
	}
	return out
}

func nws(most int) []shot {
	latest := time.Now().UTC().Add(-nwsLag).Truncate(5 * time.Minute)
	life := lifetime{until: time.Now().Add(history + frameStep)}
	var out []shot
	for i := frames - 1; i >= 0; i-- {
		at := latest.Add(-time.Duration(i) * frameStep)
		stamp := at.Format("200601021504")
		out = append(out, shot{
			at:   at,
			most: most,
			life: life,
			url:  func(z, x, y int) string { return fmt.Sprintf(iemTiles, stamp, z, x, y) },
		})
	}
	return out
}

func (r *Radar) rainviewer(ctx context.Context, most int) ([]shot, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rainIndex, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", r.o.Agent)
	resp, err := r.o.Client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("radar: RainViewer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var index struct {
		Host  string `json:"host"`
		Radar struct {
			Past []struct {
				Time int64  `json:"time"`
				Path string `json:"path"`
			} `json:"past"`
		} `json:"radar"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&index); err != nil {
		return nil, "", fmt.Errorf("radar: RainViewer index: %w", err)
	}
	past := index.Radar.Past
	past = past[max(0, len(past)-frames):]
	life := lifetime{until: time.Now().Add(history + frameStep)}
	var out []shot
	for _, p := range past {
		out = append(out, shot{
			at:   time.Unix(p.Time, 0),
			most: most,
			life: life,
			url:  func(z, x, y int) string { return fmt.Sprintf(rainTiles, index.Host, p.Path, z, x, y) },
		})
	}
	return out, "rainviewer", nil
}

func coarse(v View, most int) int {
	return max(1, min(most, int(math.Ceil(v.Zoom))-1))
}
