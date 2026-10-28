import assert from "node:assert/strict";
import test from "node:test";
import { videoPoint } from "../src/Desktop.tsx";

test("fullscreen input follows the picture instead of the letterbox", () => {
  const bounds = { left: 10, top: 20, width: 1920, height: 1080 };
  assert.deepEqual(videoPoint(bounds, 1152, 720, 106, 20), { x: 0, y: 0 });
  assert.deepEqual(videoPoint(bounds, 1152, 720, 1834, 1100), { x: 1, y: 1 });
  assert.deepEqual(videoPoint(bounds, 1152, 720, 970, 560), { x: 0.5, y: 0.5 });
  assert.ok(videoPoint(bounds, 1152, 720, 10, 560).x < 0);
});

test("portrait fullscreen keeps landscape video input aligned", () => {
  const bounds = { left: 0, top: 0, width: 1080, height: 1920 };
  assert.deepEqual(videoPoint(bounds, 1152, 720, 0, 622.5), { x: 0, y: 0 });
  assert.deepEqual(videoPoint(bounds, 1152, 720, 1080, 1297.5), { x: 1, y: 1 });
  assert.ok(videoPoint(bounds, 1152, 720, 540, 0).y < 0);
});

test("undecoded video cannot receive pointer input", () => {
  assert.equal(videoPoint({ left: 0, top: 0, width: 800, height: 500 }, 0, 0, 0, 0), undefined);
});
