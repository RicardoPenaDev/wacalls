import test from "node:test";
import assert from "node:assert/strict";
import { unixSecondsToDate, formatUnixSeconds } from "../src/lib/supportDate.ts";

test("supportDate - unixSecondsToDate converte segundos Unix para Date corretamente", () => {
  const seconds = 1726294800; // 2024-09-14
  const d = unixSecondsToDate(seconds);
  assert.ok(d instanceof Date);
  assert.equal(d.getTime(), seconds * 1000);
  assert.ok(d.getFullYear() >= 2024);
});

test("supportDate - unixSecondsToDate rejeita valores inválidos, zero ou negativos (proteção anti-1970)", () => {
  assert.equal(unixSecondsToDate(0), null);
  assert.equal(unixSecondsToDate(-1), null);
  assert.equal(unixSecondsToDate(-1000), null);
  assert.equal(unixSecondsToDate(undefined), null);
  assert.equal(unixSecondsToDate(null), null);
  assert.equal(unixSecondsToDate(NaN), null);
  assert.equal(unixSecondsToDate(Infinity), null);
  assert.equal(unixSecondsToDate(-Infinity), null);
  assert.equal(unixSecondsToDate("1726294800"), null);
});

test("supportDate - formatUnixSeconds formata corretamente datas válidas", () => {
  const seconds = 1726294800;
  const formatted = formatUnixSeconds(seconds);
  assert.ok(typeof formatted === "string");
  assert.ok(!formatted.includes("1970"), `não deve conter 1970: ${formatted}`);
  assert.ok(formatted.includes("2024") || formatted.includes("24"));
});

test("supportDate - formatUnixSeconds retorna fallback seguro para zero, nulo e inválidos", () => {
  assert.equal(formatUnixSeconds(0), "—");
  assert.equal(formatUnixSeconds(undefined), "—");
  assert.equal(formatUnixSeconds(null), "—");
  assert.equal(formatUnixSeconds(-1), "—");
  assert.equal(formatUnixSeconds(NaN), "—");
  assert.equal(formatUnixSeconds(0, {}, "N/A"), "N/A");
});
