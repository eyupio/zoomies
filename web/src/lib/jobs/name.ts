/** GitHub may send unexpanded names for jobs cancelled before matrix expansion.
 * Keep the source data intact; never invent a matrix value for display.
 */
export function readableJobText(text: string | null | undefined): string {
  return (text ?? '').replace(/\$\{\{\s*([^}]*?)\s*\}\}/g, (_, expression: string) => {
    return expression.trim() === 'matrix.projects' ? 'project unavailable' : 'value unavailable';
  });
}

export function unresolvedJobName(name: string | null | undefined): boolean {
  return readableJobText(name) !== (name ?? '');
}

/**
 * A job's name as a list row shows it: readable, and never blank. GitHub can
 * send a job with no name at all (a run cancelled before it was planned), and
 * a row that is an empty box reads as something that failed to draw. One
 * place says what such a job is called, so every list says it the same way.
 */
export function readableJobName(name: string | null | undefined): string {
  return readableJobText(name) || 'Unnamed job';
}
