/**
 * What a move to /templates carries: after a delete, the name of the
 * template that is gone, so the page can say so; the panel that asked for it
 * is gone with it.
 */
export interface TemplatesLocationState {
  deletedTemplate?: string;
}
