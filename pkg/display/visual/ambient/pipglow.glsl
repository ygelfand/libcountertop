highp float hash(highp vec2 q) {
	highp vec3 p3 = fract(vec3(q.xyx) * 0.1031);
	p3 += dot(p3, p3.yzx + 33.33);
	return fract((p3.x + p3.y) * p3.z);
}

void main() {
	highp float t = u[0];
	highp vec2 c = vtex * 2.0 - 1.0;
	highp vec2 q = crtBend(c, u[5]);
	highp vec2 uv = q * 0.5 + 0.5;
	vec2 edge = vec2(min(uv, 1.0 - uv));
	if (edge.x < 0.0 || edge.y < 0.0) {
		gl_FragColor = vec4(0.0, 0.0, 0.0, 1.0);
		return;
	}
	highp vec2 p = uv * res;
	vec3 tint = vec3(u[2], u[3], u[4]);
	float grain = float(hash(floor(p / 2.0) + fract(t * 11.0)));
	float band = sin(float(uv.y) * 3.0 + float(t) * 0.7) * 0.5 + 0.5;
	float lum = 0.07 + 0.025 * grain + 0.02 * band;
	float flicker = 0.97 + 0.03 * sin(float(t) * 53.0) + 0.02 * sin(float(t) * 7.3);
	float k = crtLight(c, uv, p, u[6], u[7], t, u[8]) * flicker;
#ifdef LIGHT
	gl_FragColor = vec4(tint * lum * 0.8, 1.0);
#else
	gl_FragColor = vec4(min((tint * lum + glow() * tint) * k, 1.0), 1.0);
#endif
}
