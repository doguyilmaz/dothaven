#!/usr/bin/env bash
#
# Reads what the release pipeline already produces, and stores nothing.
#
# GitHub counts every release asset download, and a `brew install` fetches the
# same tarball from the same URL, so both paths land in one number. Counts are
# cumulative and never expire — run this whenever, there is no history to keep.
#
# There is no active-user line here, and there cannot be. The macOS apps get
# one for free because Sparkle polls an appcast asset daily and GitHub counts
# every poll; dothaven has no updater, and Homebrew only fetches when a version
# actually changes. Manufacturing a heartbeat would mean a CLI that phones home
# — a bad trade generally, and a worse one for a tool that handles dotfiles and
# age-encrypted secrets.
#
# So the useful question here is not how many, it is WHICH: the platform split
# is the number with a decision attached, namely whether the Linux builds are
# worth shipping.
#
# Everything is a TOTAL, not a unique count, and your own downloads count too.

set -euo pipefail

REPO="${DOTHAVEN_REPO:-doguyilmaz/dothaven}"

command -v gh >/dev/null || {
    echo "gh is required: brew install gh" >&2
    exit 1
}

bold() { printf '\033[1m%s\033[0m\n' "$1"; }
rule() { printf '%s\n' "────────────────────────────────────────────────"; }

releases=$(gh api "repos/$REPO/releases" --paginate)

bold "Downloads by platform"
rule
jq -r '[.[].assets[] | select(.name | endswith(".tar.gz"))]
    | group_by(.name | sub("^dothaven_"; "") | sub("\\.tar\\.gz$"; ""))
    | sort_by(-([.[].download_count] | add))
    | .[] | "\(.[0].name | sub("^dothaven_"; "") | sub("\\.tar\\.gz$"; ""))\t\([.[].download_count] | add)"' \
    <<<"$releases" |
    while IFS=$'\t' read -r platform count; do
        printf '%-16s %6s\n' "$platform" "$count"
    done

echo
bold "Downloads by version"
rule
printf '%-10s %-12s %10s\n' "VERSION" "PUBLISHED" "DOWNLOADS"
jq -r '.[] | "\(.tag_name)\t\(.published_at[0:10])\t\([.assets[] | select(.name | endswith(".tar.gz")) | .download_count] | add // 0)"' \
    <<<"$releases" |
    while IFS=$'\t' read -r tag date count; do
        printf '%-10s %-12s %10s\n' "$tag" "$date" "$count"
    done
total=$(jq '[.[].assets[] | select(.name | endswith(".tar.gz")) | .download_count] | add // 0' <<<"$releases")
printf '%-10s %-12s %10s\n' "total" "" "$total"

echo
bold "Discovery (rolling 14 days, needs push access)"
rule
if views=$(gh api "repos/$REPO/traffic/views" 2>/dev/null); then
    printf 'views    %5s  (%s unique)\n' \
        "$(jq .count <<<"$views")" "$(jq .uniques <<<"$views")"
    clones=$(gh api "repos/$REPO/traffic/clones")
    printf 'clones   %5s  (%s unique)\n' \
        "$(jq .count <<<"$clones")" "$(jq .uniques <<<"$clones")"
else
    echo "unavailable — traffic requires push access on $REPO"
fi
printf 'stars    %5s\n' "$(gh api "repos/$REPO" --jq .stargazers_count)"
