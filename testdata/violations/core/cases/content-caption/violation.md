---
id: WID-0030
type: Widget
title: Captions that cannot be placed
---
A paragraph of text, which a caption cannot caption.

```caption
kind: figure
text: Misplaced.
```

![A figure](../../assets/figure.svg)

```caption
kind: [figure
```

![A figure](../../assets/figure.svg)

```caption
kind: figure
id: twice
text: First.
```

![A figure](../../assets/figure.svg)

```caption
kind: figure
id: twice
text: Second.
```

As [[#nowhere]] shows.
