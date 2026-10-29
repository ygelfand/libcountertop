void main() {
	highp float t = u[0];
	highp float aspect = u[1];
	highp vec2 px = pixel();
	highp vec2 uv = px / res;
	highp vec2 p = vec2((uv.x - 0.5) * aspect, uv.y - 0.32);
	float d = float(length(p));
	float mesh = step(0.5, float(fract(px.x / 4.0))) * step(0.5, float(fract(px.y / 4.0)));
	float breath = 0.55 + 0.45 * sin(float(t) * 0.6);
	vec3 col = vec3(0.03, 0.038, 0.045) + mesh * 0.012;
	col *= 1.0 - 0.55 * smoothstep(0.2, 1.1, float(length(vec2((uv.x - 0.5) * aspect, uv.y - 0.5))));
	col += vec3(0.5, 0.03, 0.02) * exp(-d * 4.5) * (0.12 + 0.12 * breath);
#ifdef LIGHT
	gl_FragColor = vec4(clamp(col, 0.0, 1.0), 1.0);
#else
	gl_FragColor = vec4(clamp(col + glow() * 0.5, 0.0, 1.0), 1.0);
#endif
}
