highp vec2 crtBend(highp vec2 c, float warp) { return c * (1.0 + warp * dot(c, c)); }

float crtLight(highp vec2 c, highp vec2 uv, highp vec2 p, float scan, float deep, highp float t, float roll) {
	vec2 edge = vec2(min(uv, 1.0 - uv));
	float sl = float(abs(fract(p.y / scan) - 0.5)) * 2.0;
	float rb = clamp(1.0 - abs(float(fract(uv.y - t * 0.06)) - 0.08) * 12.5, 0.0, 1.0);
	vec2 vc = vec2(c);
	float k = (1.0 - deep * sl) * (1.0 + 0.07 * roll * rb);
	return k * clamp(edge.x * 83.0, 0.0, 1.0) * clamp(edge.y * 83.0, 0.0, 1.0) * (1.0 - 0.28 * dot(vc, vc));
}
