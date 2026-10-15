/* Regression lock for the narrow-screen and keyboard-cursor rules.
   Run from web/: node --test tests/narrow-keyboard.test.mjs */
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const css = readFileSync(new URL("../kura.css", import.meta.url), "utf8");

test("the keyboard-active cell shows its select control", () => {
  assert.match(css, /\.cell\.is-active \.cell-check,\s*\.cell:hover \.cell-check,\s*\.cell:focus-visible \.cell-check \{\s*opacity:\s*1;/);
});

test("narrow recovery, moderation, and upload actions wrap instead of clipping", () => {
  const block = css.slice(css.indexOf("/* narrow-keyboard */"));
  assert.notEqual(block, "");
  assert.match(block, /\.recovery-heading,\s*\.recovery-row,\s*\.recovery-actions,\s*\.moderation-row,\s*\.upload-item-actions \{\s*flex-wrap:\s*wrap;/);
  assert.match(block, /min-height:\s*32px;/);
});
