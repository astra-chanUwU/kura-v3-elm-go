module Ui.Toolbar exposing (view)

import Html exposing (Html, div)
import Html.Attributes exposing (class)


view : List (Html msg) -> List (Html msg) -> Html msg
view start end =
    div [ class "view-toolbar" ]
        [ div [ class "view-toolbar-start" ] start
        , div [ class "view-toolbar-end" ] end
        ]
