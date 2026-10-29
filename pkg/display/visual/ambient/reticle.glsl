float ring(float d, float r, float w) { return smoothstep(w * 2.0, 0.0, abs(d - r)); }

void main() {
	highp float t = u[0];
	highp float aspect = u[1];
	highp vec2 px = pixel();
	highp vec2 uv = px / res;
	highp vec2 p = vec2(uv.x * aspect, uv.y);
	float w = 1.0 / float(res.y);
	vec3 red = vec3(1.0, 0.22, 0.12);
	vec3 col = vec3(0.035, 0.006, 0.004);
	highp vec2 c = vec2(uv.x < 0.5 ? 0.13 * aspect : 0.87 * aspect, uv.y < 0.5 ? 0.2 : 0.8);
	highp vec2 q = p - c;
	float d = float(length(q));
	float a = float(atan(q.y, q.x));
	float spin = float(t) * (uv.x < 0.5 ? 0.25 : -0.2);
	float marks = step(0.7, fract((a + spin) / 6.2831853 * 12.0));
	float reticle = ring(d, 0.06, w) + ring(d, 0.11, w) * 0.7 + ring(d, 0.155, w) * marks * 0.8;
	float cross = (smoothstep(w * 2.0, 0.0, abs(float(q.x))) + smoothstep(w * 2.0, 0.0, abs(float(q.y)))) *
		step(d, 0.18) * step(0.02, d);
	col += red * (reticle + cross * 0.6) * 0.32;
	highp vec2 m = vec2((uv.x - 0.5) * aspect, uv.y - 0.5);
	float dm = float(length(m));
	float am = float(atan(m.y, m.x)) / 6.2831853 + 0.5;
	float sweep = fract(am - float(t) * 0.08);
	col += red * smoothstep(0.85, 1.0, sweep) * smoothstep(0.75, 0.0, dm) * 0.12;
	col += red * ring(dm, 0.62, w) * 0.18 + red * ring(dm, 0.66, w * 3.0) * step(0.6, fract(am * 90.0)) * 0.12;
	float scan = 0.85 + 0.15 * sin(float(px.y) * 1.5708);
	col *= scan;
#ifdef LIGHT
	gl_FragColor = vec4(clamp(col, 0.0, 1.0), 1.0);
#else
	gl_FragColor = vec4(clamp(col + glow() * 0.6, 0.0, 1.0), 1.0);
#endif
}
