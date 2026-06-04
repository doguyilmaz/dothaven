import { join } from "node:path";
import { getHome } from "./home";

export async function resolveOutputDir(explicit: string | null): Promise<string> {
  if (explicit) return explicit;

  const cwd = process.cwd();
  const isRepo = await Bun.file(join(cwd, ".git/HEAD")).exists();

  if (isRepo) return join(cwd, "reports");

  return join(getHome(), "Downloads");
}
