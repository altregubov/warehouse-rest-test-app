# GitHub CLI (`gh`) Issue Reference

This reference documents the low-level GitHub CLI (`gh`) commands used by the issue skill for querying, creating, editing, and closing issues across repositories.

---

## 1. Repository Targeting

### Current Repository (Default)
When executed inside any git clone with a GitHub remote (e.g., `origin`) or with a default repository configured via `gh repo set-default`, `gh` commands natively target the current repository without requiring the `-R` flag:
```bash
gh issue list
gh issue view <issue-number>
```

### Explicit Repository Override
To target a specific repository explicitly regardless of current working directory, specify `-R` / `--repo`:
```bash
gh issue <command> -R <owner/repo> ...
```
Or export the environment variable:
```bash
export GH_REPO="<owner/repo>"
```

---

## 2. Listing and Querying Issues

### List Open Issues
```bash
gh issue list [ -R <owner/repo> ]
```

### List Issues in JSON Format
```bash
gh issue list [ -R <owner/repo> ] --json number,title,state,labels,assignees
```

### View Single Issue Details
```bash
gh issue view <issue-number> [ -R <owner/repo> ]
```

### Fetch Issue Body Only (Raw Markdown)
```bash
gh issue view <issue-number> [ -R <owner/repo> ] --json body --jq .body
```

---

## 3. Creating Issues

### Create with Title and Body File
```bash
gh issue create [ -R <owner/repo> ] \
  --title "feat: implement issue management skill" \
  --body-file .agents/skills/issue/templates/issue_template.md
```

### Create with Inline Body and Labels
```bash
gh issue create [ -R <owner/repo> ] \
  --title "fix: correct task check logic" \
  --label "bug" \
  --body "## Overview\nFixes issue with task checking."
```

### Create with Media Attached
```bash
gh issue create [ -R <owner/repo> ] \
  --title "feat: add screenshot preview" \
  --body-file issue_body.md \
  --attach "./preview.png#Preview Screenshot"
```

---

## 4. Updating Issues & Task Checklists

### Edit Body Directly
```bash
gh issue edit <issue-number> [ -R <owner/repo> ] --body-file <updated-body.md>
```

### Add or Remove Labels
```bash
gh issue edit <issue-number> [ -R <owner/repo> ] --add-label "in-progress" --remove-label "todo"
```

### Post a Progress Comment
```bash
gh issue comment <issue-number> [ -R <owner/repo> ] --body-file <progress-comment.md>
```

---

## 5. Attaching Media (Images & Videos)

GitHub CLI natively supports attaching images (`.png`, `.jpg`, `.jpeg`, `.gif`, `.webp`) and videos (`.mp4`, `.mov`, `.webm`):
- Format: `--attach '<path_to_file>#<Alt Text>'`
- Multiple files: Repeat `--attach` (up to 50 files).
- When the body references the file as `![Alt Text](./path_to_file.png)`, `gh` replaces the relative path with the GitHub uploaded CDN asset URL automatically.

### Example: Attaching to Existing Issue
```bash
gh issue edit <issue-number> [ -R <owner/repo> ] \
  --attach "./artifacts/screenshot.png#Verification Screenshot"
```

### Example: Attaching to a Comment
```bash
gh issue comment <issue-number> [ -R <owner/repo> ] \
  --body "Attached demo of completed functionality:" \
  --attach "./demo.mp4"
```

---

## 6. Closing Issues with Pushed Commits

### 1. Close via Commit Message Keywords
Commit messages pushed to the default branch automatically close linked issues when using GitHub keywords:
- `Fixes #<number>`
- `Closes #<number>`
- `Resolves #<number>`

Example:
```bash
git commit -m "feat: complete issue tracking skill

Closes #1"
git push origin main
```

### 2. Explicit CLI Close after Push
```bash
COMMIT_SHA=$(git rev-parse --short HEAD)
gh issue close <issue-number> [ -R <owner/repo> ] \
  --reason "completed" \
  --comment "Resolved in commit ${COMMIT_SHA}. Pushed to origin."
```

### 3. Verify Status
```bash
gh issue view <issue-number> [ -R <owner/repo> ] --json number,title,state,stateReason
```
Confirm `state` is `"CLOSED"` and `stateReason` is `"COMPLETED"`.
