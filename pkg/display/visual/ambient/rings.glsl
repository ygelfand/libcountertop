void main() {
	highp float t = u[0];
	highp float aspect = u[1];
	highp vec2 px = pixel();
	highp vec2 uv = px / res;
	highp vec2 p = (uv - 0.5) * vec2(aspect, 1.0);
	float d = float(length(p));
	float a = float(atan(p.y, p.x)) / 6.2831853 + 0.5;
	float px1 = 1.0 / float(res.y);
	vec3 cyan = vec3(0.2, 0.85, 0.95);
	vec3 col = vec3(0.008, 0.025, 0.035) * (1.0 - 0.6 * d);
	for (int i = 0; i < 6; i++) {
		float fi = float(i);
		float r = 0.16 + 0.12 * fi;
		float w = px1 * (1.2 + mod(fi, 2.0));
		float ring = smoothstep(w * 2.0, 0.0, abs(d - r));
		float dir = mod(fi, 2.0) * 2.0 - 1.0;
		float segs = 3.0 + fi * 2.0;
		float dash = step(0.35, fract((a + float(t) * 0.012 * dir * (1.0 + fi * 0.4)) * segs));
		col += cyan * ring * mix(0.08, 0.42, dash) * (1.0 - fi * 0.11);
	}
	float tick = step(0.82, fract(a * 120.0)) * smoothstep(px1 * 14.0, 0.0, abs(d - 0.86));
	col += cyan * tick * 0.25;
	float sweep = fract(a - float(t) * 0.05);
	col += cyan * smoothstep(0.92, 1.0, sweep) * smoothstep(0.9, 0.1, d) * 0.08;
#ifdef LIGHT
	gl_FragColor = vec4(clamp(col, 0.0, 1.0), 1.0);
#else
	gl_FragColor = vec4(clamp(col + glow() * 0.6, 0.0, 1.0), 1.0);
#endif
}
