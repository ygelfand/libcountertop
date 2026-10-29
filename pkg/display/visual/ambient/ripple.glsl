void main() {
	highp float t = u[0];
	highp float aspect = u[1];
	highp vec2 px = pixel();
	highp vec2 uv = px / res;
	highp vec2 p = (uv - 0.5) * vec2(aspect, 1.0);
	float d = float(length(p));
	float wave = pow(0.5 + 0.5 * sin((d - float(t) * 0.035) * 38.0), 36.0);
	float rings = pow(0.5 + 0.5 * sin(d * 9.0), 60.0);
	vec3 base = mix(vec3(0.1, 0.15, 0.18), vec3(0.035, 0.055, 0.07), smoothstep(0.0, 0.9, d));
	vec3 col = base + vec3(0.25, 0.65, 0.75) * wave * 0.06 * exp(-d * 1.5);
	col += vec3(0.4, 0.75, 0.85) * rings * 0.035;
#ifdef LIGHT
	gl_FragColor = vec4(clamp(col, 0.0, 1.0), 1.0);
#else
	gl_FragColor = vec4(clamp(col + glow() * 0.4, 0.0, 1.0), 1.0);
#endif
}
