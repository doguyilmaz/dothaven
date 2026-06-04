import { collect } from "./commands/collect";
import { backup } from "./commands/backup";
import { scan } from "./commands/scan";
import { security } from "./commands/security";
import { restore } from "./commands/restore";
import { diff } from "./commands/diff";
import { status } from "./commands/status";
import { compareCli } from "./commands/compare";
import { list } from "./commands/list";

const [command, ...args] = Bun.argv.slice(2);

switch (command) {
  case "collect":
    await collect(args);
    break;
  case "backup":
    await backup(args);
    break;
  case "scan":
    await scan(args);
    break;
  case "security":
    await security(args);
    break;
  case "restore":
    await restore(args);
    break;
  case "diff":
    await diff(args);
    break;
  case "status":
    await status();
    break;
  case "compare":
    await compareCli(args);
    break;
  case "list":
    await list(args);
    break;
  default:
    console.log(`Usage: dotfiles <command>

Commands:
  collect [--no-redact] [--slim] [-o path]            Collect machine config → .dotf report
  backup  [--no-redact] [--archive] [--only a,b] [--skip c,d] [-o path]
                                                       Backup config files (--archive for .tar.gz)
  scan     [path]                                    Scan files for sensitive data (console)
  security [path] [-o file]                          Write a Markdown security report (default SECURITY.md)
  restore <path> [--pick] [--dry-run]                Restore config files from backup
  diff    [path] [--section <name>]                  Compare backup against live system
  status                                             Quick summary of backup state
  compare [file1] [file2]                            Diff two .dotf reports
  list <section>                                     Print a section from most recent report`);
}
