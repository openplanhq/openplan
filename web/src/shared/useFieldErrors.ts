import { useCallback, useState } from "react";
import { fieldErrors } from "../api/problems";

// useFieldErrors holds the field errors a refused request put on a form's
// fields, for the pointers under prefix. Editing a field clears its error,
// since what the server said no longer describes what is typed. set puts
// errors found before any request, such as a value that isn't valid JSON.
export function useFieldErrors(prefix: string) {
  const [errors, setErrors] = useState<Record<string, string>>({});
  const show = useCallback((error: unknown) => setErrors(fieldErrors(error, prefix)), [prefix]);
  const set = useCallback((next: Record<string, string>) => setErrors(next), []);
  const clear = useCallback((field?: string) => {
    setErrors((current) => {
      if (field === undefined) {
        return Object.keys(current).length === 0 ? current : {};
      }
      if (!(field in current)) {
        return current;
      }
      const { [field]: _cleared, ...rest } = current;
      return rest;
    });
  }, []);
  return { errors, show, set, clear };
}
