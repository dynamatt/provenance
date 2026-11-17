---
id: QRY-0005
type: QueryReport
title: The widget it is about and its children
about: WID-0025
scope:
  from: Widget
  where:
    any_of:
      - field: id
        operator: equals
        value_of: ^about
      - field: parent
        operator: equals
        value_of: ^about
  order_by: id
---
