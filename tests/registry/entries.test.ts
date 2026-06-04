import { test, expect, describe } from "bun:test";
import { registryEntries } from "../../src/registry/entries";
import { resolvePath, getEntriesForPlatform } from "../../src/registry/resolve";

describe("registry entries", () => {
  test("all IDs are unique", () => {
    const ids = registryEntries.map((e) => e.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  test("all backupDest are unique", () => {
    const dests = registryEntries.map((e) => e.backupDest);
    expect(new Set(dests).size).toBe(dests.length);
  });

  test("all entries have at least one platform path", () => {
    for (const entry of registryEntries) {
      const pathCount = Object.keys(entry.paths).length;
      expect(pathCount).toBeGreaterThan(0);
    }
  });

  test("all entries have valid category", () => {
    const validCategories = ["ai", "shell", "git", "editor", "terminal", "ssh", "npm", "bun", "cloud", "secrets"];
    for (const entry of registryEntries) {
      expect(validCategories).toContain(entry.category);
    }
  });

  test("all paths use ~ or %ENV_VAR% prefix", () => {
    for (const entry of registryEntries) {
      for (const path of Object.values(entry.paths)) {
        const valid = path!.startsWith("~") || path!.startsWith("%");
        expect(valid).toBe(true);
      }
    }
  });
});

describe("resolvePath", () => {
  test("resolves ~ to home directory", () => {
    const entry = registryEntries.find((e) => e.id === "shell.zshrc")!;
    const resolved = resolvePath(entry, "/Users/test");
    expect(resolved).toBe("/Users/test/.zshrc");
  });

  test("returns null for missing platform", () => {
    const entry = { ...registryEntries[0], paths: { win32: "C:\\test" as string } };
    if (process.platform !== "win32") {
      const resolved = resolvePath(entry as any, "/Users/test");
      expect(resolved).toBeNull();
    }
  });
});

describe("getEntriesForPlatform", () => {
  test("filters entries for current platform", () => {
    const filtered = getEntriesForPlatform(registryEntries);
    expect(filtered.length).toBe(registryEntries.length);
  });
});
