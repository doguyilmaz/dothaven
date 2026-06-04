import { test, expect, describe } from "bun:test";
import { registryEntries } from "../../src/registry/entries";

const byId = (id: string) => registryEntries.find((e) => e.id === id);

describe("cloud + secret registry entries", () => {
  test("credential files/dirs are high sensitivity (encrypted on chezmoi-export)", () => {
    expect(byId("cloud.aws.credentials")?.sensitivity).toBe("high");
    expect(byId("cloud.kube.config")?.sensitivity).toBe("high");
    expect(byId("cloud.docker.config")?.sensitivity).toBe("high");
    expect(byId("secrets.gnupg")?.sensitivity).toBe("high");
  });

  test("non-secret cloud config is medium", () => {
    expect(byId("cloud.aws.config")?.sensitivity).toBe("medium");
  });

  test("gnupg + gcloud configurations are directories (declarative, no-op until present)", () => {
    expect(byId("secrets.gnupg")?.kind.type).toBe("dir");
    expect(byId("cloud.gcloud.configurations")?.kind.type).toBe("dir");
  });

  test("every registry entry id is unique", () => {
    const ids = registryEntries.map((e) => e.id);
    expect(new Set(ids).size).toBe(ids.length);
  });
});
