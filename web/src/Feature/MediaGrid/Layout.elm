module Feature.MediaGrid.Layout exposing
    ( Geometry
    , Viewport
    , cellTop
    , firstVisibleIndex
    , geometry
    , isFullyVisible
    , left
    , revealTop
    , rowsPerPage
    , totalHeight
    , visibleRange
    )

{-| Uniform square cells stretched to fill the row, so every position is
arithmetic and the grid can be virtualized.
-}


type alias Geometry =
    { columns : Int
    , cell : Float
    , gap : Float
    , rowHeight : Float
    , padding : Float
    }


type alias Viewport =
    { scrollTop : Float
    , width : Float
    , height : Float
    }


geometry : Float -> Int -> Geometry
geometry width thumb =
    let
        gap =
            if thumb <= 144 then
                8

            else
                16

        padding =
            16

        inner =
            max 0 (width - 2 * padding)

        columns =
            max 1 (floor ((inner + gap) / (toFloat thumb + gap)))

        cell =
            max 1 ((inner - gap * toFloat (columns - 1)) / toFloat columns)
    in
    { columns = columns, cell = cell, gap = gap, rowHeight = cell + gap, padding = padding }


rows : Geometry -> Int -> Int
rows g count =
    (count + g.columns - 1) // g.columns


totalHeight : Geometry -> Int -> Float
totalHeight g count =
    if count <= 0 then
        0

    else
        2 * g.padding + toFloat (rows g count) * g.rowHeight - g.gap


left : Geometry -> Int -> Float
left g index =
    g.padding + toFloat (modBy g.columns index) * (g.cell + g.gap)


cellTop : Geometry -> Int -> Float
cellTop g index =
    g.padding + toFloat (index // g.columns) * g.rowHeight


{-| Index range `[from, to)` covering the viewport plus `overscan` rows on
either side.
-}
visibleRange : Geometry -> Viewport -> Int -> Int -> ( Int, Int )
visibleRange g viewport count overscan =
    let
        firstRow =
            max 0 (floor ((viewport.scrollTop - g.padding) / g.rowHeight) - overscan)

        lastRow =
            min (rows g count - 1) (floor ((viewport.scrollTop + viewport.height - g.padding) / g.rowHeight) + overscan)
    in
    ( firstRow * g.columns, min count ((lastRow + 1) * g.columns) )


firstVisibleIndex : Geometry -> Float -> Int
firstVisibleIndex g scrollTop =
    max 0 (ceiling ((scrollTop - g.padding) / g.rowHeight)) * g.columns


isFullyVisible : Geometry -> Viewport -> Int -> Bool
isFullyVisible g viewport index =
    let
        top =
            cellTop g index
    in
    top >= viewport.scrollTop && top + g.cell <= viewport.scrollTop + viewport.height


{-| The scroll offset that reveals `index` with the least movement.
-}
revealTop : Geometry -> Viewport -> Int -> Float
revealTop g viewport index =
    let
        top =
            cellTop g index
    in
    if top < viewport.scrollTop then
        max 0 (top - g.padding)

    else if top + g.cell > viewport.scrollTop + viewport.height then
        top + g.cell + g.padding - viewport.height

    else
        viewport.scrollTop


rowsPerPage : Geometry -> Float -> Int
rowsPerPage g height =
    max 1 (floor (height / g.rowHeight))
