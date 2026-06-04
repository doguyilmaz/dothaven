import { test, expect, describe } from "bun:test";
import { planInit, isReady, repoUrl, type InitState } from "../../src/commands/init";

const base: InitState = {
  chezmoiInstalled: false,
  ageKeyConfigured: false,
  sourceInitialized: false,
};

describe("repoUrl", () => {
  test("uses the gh login; private repo is named 'dotfiles' (chezmoi convention)", () => {
    expect(repoUrl("doguyilmaz")).toBe("git@github.com:doguyilmaz/dotfiles.git");
  });
  test("falls back to a placeholder when the user is unknown", () => {
    expect(repoUrl()).toBe("git@github.com:<you>/dotfiles.git");
  });
});

describe("planInit", () => {
  test("nothing set up → all three steps todo with their commands", () => {
    const steps = planInit(base);
    expect(steps.map((s) => s.id)).toEqual(["chezmoi", "age-key", "source"]);
    expect(steps.every((s) => s.status === "todo")).toBe(true);
    expect(steps[0].command).toBe("brew install chezmoi");
    expect(steps[1].command).toContain("age-keygen");
    expect(steps[1].note).toContain("Back this key up"); // loss warning present
    expect(steps[2].command).toContain("chezmoi init");
  });

  test("uses the resolved user in the chezmoi init command", () => {
    const steps = planInit({ ...base, chezmoiInstalled: true, ageKeyConfigured: true, user: "doguyilmaz" });
    const source = steps.find((s) => s.id === "source");
    expect(source?.command).toBe("chezmoi init git@github.com:doguyilmaz/dotfiles.git");
  });

  test("done steps carry no command or note", () => {
    const steps = planInit({ chezmoiInstalled: true, ageKeyConfigured: true, sourceInitialized: true });
    expect(steps.every((s) => s.status === "done")).toBe(true);
    expect(steps.every((s) => s.command === undefined && s.note === undefined)).toBe(true);
  });
});

describe("isReady", () => {
  test("true only when every prerequisite is done", () => {
    expect(isReady(planInit(base))).toBe(false);
    expect(isReady(planInit({ chezmoiInstalled: true, ageKeyConfigured: true, sourceInitialized: true }))).toBe(true);
    expect(isReady(planInit({ ...base, chezmoiInstalled: true }))).toBe(false);
  });
});
