import { test, expect, describe } from "bun:test";
import { formatReport } from "../../src/scan/report";
import { scanContent, summarize } from "../../src/scan/scanner";

describe("formatReport", () => {
  test("returns empty string for no findings", () => {
    const summary = summarize([scanContent("clean", "no sensitive data")]);
    expect(formatReport(summary)).toBe("");
  });

  test("formats report with findings", () => {
    const results = [
      scanContent("~/.ssh/id_rsa", "-----BEGIN RSA PRIVATE KEY-----"),
      scanContent("~/.npmrc", "_authToken=secret"),
      scanContent("~/.gitconfig", "email = dev@example.com"),
    ];
    const report = formatReport(summarize(results));

    expect(report).toContain("⚠ Sensitivity report:");
    expect(report).toContain("HIGH");
    expect(report).toContain("MEDIUM");
    expect(report).toContain("private key");
    expect(report).toContain("skipped");
    expect(report).toContain("redacted");
    expect(report).toContain("--no-redact");
  });

  test("shows counts in summary line", () => {
    const results = [scanContent("a", "-----BEGIN RSA PRIVATE KEY-----"), scanContent("b", "_authToken=secret")];
    const report = formatReport(summarize(results));
    expect(report).toContain("1 items redacted");
    expect(report).toContain("1 skipped");
  });
});
