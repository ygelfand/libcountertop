package visual

import (
	_ "embed"
	"math"

	"github.com/ygelfand/libcountertop/pkg/display/ui"
)

const (
	AmbientPip3    Kind = "ambient-pipboy-3"
	AmbientPipNV   Kind = "ambient-pipboy-nv"
	AmbientShrine  Kind = "ambient-shrine"
	AmbientRings   Kind = "ambient-rings"
	AmbientGrid    Kind = "ambient-grid"
	AmbientReticle Kind = "ambient-reticle"
	AmbientEye     Kind = "ambient-eye"
	AmbientRipple  Kind = "ambient-ripple"
)

var (
	//go:embed ambient/pipglow.glsl
	pipGlowBody string
	//go:embed ambient/shrine.glsl
	shrineShader string
	//go:embed ambient/rings.glsl
	ringsShader string
	//go:embed ambient/grid.glsl
	gridShader string
	//go:embed ambient/reticle.glsl
	reticleShader string
	//go:embed ambient/eye.glsl
	eyeShader string
	//go:embed ambient/ripple.glsl
	rippleShader string
)

func init() {
	for k, shader := range map[Kind]string{
		AmbientShrine:  shrineShader,
		AmbientRings:   ringsShader,
		AmbientGrid:    gridShader,
		AmbientReticle: reticleShader,
		AmbientEye:     eyeShader,
		AmbientRipple:  rippleShader,
	} {
		register(k, func() Visual { return &ambient{shader: shader} })
	}
	for k, screen := range map[Kind]Kind{AmbientPip3: PipBoy3, AmbientPipNV: PipBoyNV} {
		crt, _ := CRTOf(screen)
		register(k, func() Visual {
			return &ambient{shader: crtShader + pipGlowBody, extra: func(u float64) []float32 {
				return []float32{
					float32(crt.Tint[0]), float32(crt.Tint[1]), float32(crt.Tint[2]),
					float32(crt.Warp), float32(crt.Scan * u), float32(crt.Deep), float32(crt.Roll),
				}
			}}
		})
	}
}

type ambient struct {
	st     stage
	shader string
	extra  func(u float64) []float32
}

func (v *ambient) Shade(g GL, fresh bool, in ui.Rect, x Input) error {
	if !v.st.measure(in) {
		return nil
	}
	if fresh {
		if err := g.Program(v.shader, Light); err != nil {
			return err
		}
	}
	vals := []float32{
		float32(math.Mod(x.Now.Seconds(), 3600)),
		float32(v.st.w) / float32(v.st.h),
	}
	if v.extra != nil {
		vals = append(vals, v.extra(v.st.u)...)
	}
	return g.Values(vals, 0.4, 0.05, 2)
}
