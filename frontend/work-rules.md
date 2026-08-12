# Frontend Work Rules

The rule is written once in `work-rules.md` at the repository root. Restated here
in short, because a delegated frontend run may never open that file.

| rule | detail |
| :- | :- |
| how many | one delegated run for this stack, one for the backend, two in total |
| ceiling | 60 minutes of processing per run, not per step |
| on reaching it | stop where the work is, start nothing new, write the report, then return |
| report name | `AGENT-TIMEOUT-FRONTEND-REPORT-YYYY_MM_DD_hh_mm_ss.md` |
| location | the repository root, not `frontend/`, so both stacks and the developer see it in one place |
| not ignored | deliberately absent from every `.gitignore` |
| before starting | read the root file, then any existing timeout report |

The report says what finished, what was mid-change file by file, what commands
ran and how they exited, what is blocked, what remains in order, and why the run
is suspected to have overrun.

**The 60 above can change.** It is stated in full at the root and only copied
here. A run reads the root file before starting rather than trusting this copy or
a remembered value, and a tool holding the old number in a stored note corrects
that note from the root file.

<br>

## Further Reading

| file | contents |
| :- | :- |
| `../work-rules.md` | the full rule, why it exists, and the report format |
| `phase-track.md` | the frontend phases, with the checkboxes a run ticks |
