import { hostname } from "node:os";
import type { Collector } from "./types";
import { makeSection } from "./types";

export const collectMeta: Collector = async () => {
  const host = hostname();
  const os = `${(await Bun.$`uname -s`.text()).trim()} ${(await Bun.$`uname -m`.text()).trim()}`;
  const date = new Date().toISOString().split("T")[0];

  return {
    meta: makeSection("meta", {
      pairs: { host, os, date },
    }),
  };
};
