highp float rounded(highp vec2 p, highp vec2 c, highp vec2 ext, highp float r) {
	highp vec2 q = abs(p - c) - ext + r;
	return length(max(q, 0.0)) + min(max(q.x, q.y), 0.0) - r;
}

vec3 photo(sampler2D sharp, sampler2D soft, highp vec2 px, highp vec4 dst, highp vec4 src, float back,
	highp vec4 blur, highp float mat, highp float shade, vec3 bg, vec3 matc) {
	vec3 col = bg;
	if (back > 0.5 && back < 1.5) col = texture2D(soft, blur.xy + px / res * blur.zw).rgb * 0.59;
	highp vec2 c = dst.xy + dst.zw * 0.5;
	if (mat > 0.0) {
		highp vec2 ext = dst.zw * 0.5 + mat;
		highp float d = rounded(px - vec2(0.0, shade / 3.0), c, ext, 0.0);
		col *= 1.0 - 0.5 * (1.0 - smoothstep(-shade * 0.2, shade, d));
		if (rounded(px, c, ext, 0.0) <= 0.0) col = matc;
	}
	highp vec2 l = (px - dst.xy) / dst.zw;
	if (l.x >= 0.0 && l.y >= 0.0 && l.x <= 1.0 && l.y <= 1.0) col = texture2D(sharp, src.xy + l * src.zw).rgb;
	return col;
}

vec3 shotA(highp vec2 px, vec3 bg, vec3 matc) {
	if (u[35] < 0.5) return bg;
	return photo(t0, t2, px, vec4(u[20], u[21], u[22], u[23]), vec4(u[24], u[25], u[26], u[27]), u[28],
		vec4(u[29], u[30], u[31], u[32]), u[33], u[34], bg, matc);
}

vec3 shotB(highp vec2 px, vec3 bg, vec3 matc) {
	if (u[51] < 0.5) return bg;
	return photo(t1, t3, px, vec4(u[36], u[37], u[38], u[39]), vec4(u[40], u[41], u[42], u[43]), u[44],
		vec4(u[45], u[46], u[47], u[48]), u[49], u[50], bg, matc);
}

vec3 shot(float k, highp vec2 px, vec3 bg, vec3 matc) {
	return k < 0.5 ? shotA(px, bg, matc) : shotB(px, bg, matc);
}

void main() {
	highp vec2 px = pixel();
	vec3 bg = vec3(u[4], u[5], u[6]);
	vec3 matc = vec3(u[14], u[15], u[16]);
	float n = u[2];
	float o = 1.0 - n;
	float p = clamp(u[0], 0.0, 1.0);
	float e = p * p * (3.0 - 2.0 * p);
	vec3 col;
	if (u[3] < 0.5) {
		col = shot(n, px, bg, matc);
	} else if (u[1] > 1.5 && u[1] < 2.5) {
		highp float edge = res.x * (1.0 - e);
		col = px.x < edge ? shot(o, px + vec2(res.x * e, 0.0), bg, matc) : shot(n, px - vec2(edge, 0.0), bg, matc);
	} else if (u[1] > 2.5 && u[1] < 3.5) {
		highp vec2 c = res * 0.5;
		vec3 was = shot(o, c + (px - c) * mix(1.0, 0.94, e), bg, matc);
		vec3 now = shot(n, c + (px - c) / mix(1.12, 1.0, e), bg, matc);
		col = mix(was, now, e);
	} else if (u[1] > 3.5 && u[1] < 4.5) {
		highp float w = res.x * 0.12;
		highp float edge = mix(-w * 0.5, res.x + w * 0.5, e);
		float a = 1.0 - smoothstep(edge - w * 0.5, edge + w * 0.5, px.x);
		col = mix(shot(o, px, bg, matc), shot(n, px, bg, matc), a);
	} else if (u[1] > 4.5) {
		highp float h = fract(sin(dot(floor(px / 3.0), vec2(12.9898, 78.233))) * 43758.5453);
		float a = smoothstep(h - 0.08, h + 0.08, e * 1.16 - 0.08);
		col = mix(shot(o, px, bg, matc), shot(n, px, bg, matc), a);
	} else {
		col = mix(shot(o, px, bg, matc), shot(n, px, bg, matc), e);
	}
	if (u[7] > 0.0) {
		highp vec2 c = vec2(u[8] + u[10] * 0.5, u[9] + u[11] * 0.5);
		highp float pad = u[12];
		highp float soft = u[13];
		highp float d = rounded(px, c, vec2(u[10], u[11]) * 0.5 + pad, pad + soft * 0.5);
		float t = clamp(1.0 - d / soft, 0.0, 1.0);
		float k = d <= 0.0 ? 1.0 : t * t * (3.0 - 2.0 * t);
		float a = u[7] * k;
		col = mix(col, bg, a);
	}
	gl_FragColor = vec4(col, 1.0);
}
