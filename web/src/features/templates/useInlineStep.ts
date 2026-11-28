import { useEffect, useRef, useState } from "react";

// The state of a step the template panel opens in place, such as Delete's
// question or Change ref's form, and the refusal it ends on. The panel stays
// mounted from one template to the next, so both belong to the template they
// were asked about: the step closes when the template changes, and coming
// back finds neither. An answer acts only while stillOpen(): one that lands
// after the person moved on, or opened the other step, leaves them where they
// are. stillHere() is the weaker check, for an answer that ends the template
// itself and so outranks whichever step is open.
export function useInlineStep<Refusal>(id: string) {
  const triggerRef = useRef<HTMLButtonElement>(null);
  const [openFor, setOpenFor] = useState<string | null>(null);
  // openFor as of now, for an answer that arrives after renders moved on.
  const openNow = useRef<string | null>(null);
  const [refusal, setRefusal] = useState<{ id: string; refusal: Refusal } | null>(null);
  // The template on screen now, for the same reason.
  const idNow = useRef(id);

  function close() {
    openNow.current = null;
    setOpenFor(null);
  }
  function dismiss() {
    close();
    setRefusal(null);
  }
  useEffect(() => {
    idNow.current = id;
    dismiss();
  }, [id]);

  return {
    triggerRef,
    open: openFor === id,
    refusal: refusal?.id === id ? refusal.refusal : null,
    ask() {
      openNow.current = id;
      setRefusal(null);
      setOpenFor(id);
    },
    // Another step opening in its place: close, and leave focus where it is.
    dismiss,
    cancel() {
      dismiss();
      triggerRef.current?.focus();
    },
    stillOpen: () => openNow.current === id,
    stillHere: () => idNow.current === id,
    close,
    refuse: (next: Refusal | null) => setRefusal(next === null ? null : { id, refusal: next })
  };
}
