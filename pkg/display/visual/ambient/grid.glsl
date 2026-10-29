float gridline(highp float v, float width) {
	float f = float(fract(v));
	return clamp(1.0 - min(f, 1.0 - f) / width, 0.0, 1.0);
}

void main() {
	highp float t = u[0];
	highp float aspect = u[1];
	highp vec2 px = pixel();
	highp vec2 uv = px / res;
	vec3 cyan = vec3(0.15, 0.8, 0.95);
	float horizon = 0.46;
	vec3 col = vec3(0.004, 0.02, 0.03);
	float below = float(uv.y) - horizon;
	if (below > 0.0) {
		highp float z = 0.35 / below;
		highp float x = (uv.x - 0.5) * aspect * z;
		highp float gz = z + t * 0.6;
		float w = clamp(0.025 * float(z), 0.02, 0.45);
		float grid = max(gridline(x * 1.6, w), gridline(gz * 1.6, w));
		float fade = exp(-float(z) * 0.18);
		col += cyan * grid * fade * 0.55;
	} else {
		highp vec2 sky = vec2(uv.x * aspect, uv.y) * 14.0;
		float g = max(gridline(sky.x, 0.02), gridline(sky.y, 0.02));
		col += cyan * g * 0.03 * smoothstep(0.0, horizon, float(uv.y));
	}
	col += cyan * exp(-abs(below) * 28.0) * 0.35;
	col += vec3(0.05, 0.25, 0.3) * exp(-abs(below) * 6.0) * 0.25;
#ifdef LIGHT
	gl_FragColor = vec4(clamp(col, 0.0, 1.0), 1.0);
#else
	gl_FragColor = vec4(clamp(col + glow() * 0.7, 0.0, 1.0), 1.0);
#endif
}
