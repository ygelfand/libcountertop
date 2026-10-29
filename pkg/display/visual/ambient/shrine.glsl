highp float hash(highp vec2 q) {
	highp vec3 p3 = fract(vec3(q.xyx) * 0.1031);
	p3 += dot(p3, p3.yzx + 33.33);
	return fract((p3.x + p3.y) * p3.z);
}

highp vec2 cell(highp vec2 p) {
	highp vec2 r = vec2(1.0, 1.7320508);
	highp vec2 h = r * 0.5;
	highp vec2 a = mod(p, r) - h;
	highp vec2 b = mod(p - h, r) - h;
	return dot(a, a) < dot(b, b) ? a : b;
}

void main() {
	highp float t = u[0];
	highp float aspect = u[1];
	highp vec2 px = pixel();
	highp vec2 uv = px / res;
	highp vec2 p = (uv - 0.5) * vec2(aspect, 1.0);
	highp vec2 g = cell(p * 11.0);
	vec2 q = abs(vec2(g));
	float e = max(dot(q, vec2(0.5, 0.8660254)), q.x);
	float edge = smoothstep(0.455, 0.5, e);
	float d = float(length(p));
	float wave = exp(-pow((d - fract(float(t) * 0.07) * 1.4) * 9.0, 2.0));
	highp vec2 id = floor(p * 11.0 + 0.5);
	float spark = step(0.965, float(hash(id))) * (0.5 + 0.5 * sin(float(t) * 1.7 + float(hash(id + 3.1)) * 40.0));
	float stone = float(hash(floor(px / 3.0))) * 0.025;
	vec3 base = vec3(0.02, 0.05, 0.085) + stone;
	vec3 cyan = vec3(0.15, 0.75, 1.0);
	float vignette = smoothstep(1.0, 0.2, d);
	vec3 col = base * (0.55 + 0.45 * vignette);
	col += cyan * edge * (0.05 + 0.3 * wave) * vignette;
	col += cyan * spark * edge * 0.35;
#ifdef LIGHT
	gl_FragColor = vec4(clamp(col, 0.0, 1.0), 1.0);
#else
	gl_FragColor = vec4(clamp(col + glow() * 0.6, 0.0, 1.0), 1.0);
#endif
}
