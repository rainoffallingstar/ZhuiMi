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
    size: 10.5pt,
  )
  body
}
