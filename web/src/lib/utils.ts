import { createCn } from "cn/config"

// cn, told about the steps theme.css adds to Tailwind's scales, so a later
// class replaces an earlier one of the same kind (text-meta replaces a
// component's text-sm) as it does for the built-in steps. Without this, both
// classes stay and the stylesheet's order decides which applies.
export const cn = createCn({
  extend: {
    theme: {
      text: ["page-title", "title", "wordmark", "section-title", "label", "body", "meta", "caption", "log"],
      radius: ["panel"],
      leading: ["label"],
      container: ["beside-slug"],
    },
  },
})
