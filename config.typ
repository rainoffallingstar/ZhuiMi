#let tufted = none

#let template(title: "追觅日报", body) = {
  set document(title: title)
  set page(
    paper: "a4",
    margin: (
      x: 22mm,
      y: 20mm,
    ),
  )
  set text(
    lang: "zh",
    font: (
      "Noto Serif CJK SC",
      "Noto Sans CJK SC",
      "Noto Serif CJK",
      "Noto Sans CJK",
      "DejaVu Sans",
    ),
    size: 10.5pt,
  )
  body
}
