module Ui.Help exposing (view)

import Html exposing (Html, details, div, summary, text)
import Html.Attributes exposing (attribute, class, id)
import Ui.Icon


{-| Background guidance that is available on hover, focus, or a tap.
Keep action-critical errors and warnings in the main interface instead.
-}
view : String -> String -> String -> Html msg
view helpId label explanation =
    div [ class "context-help" ]
        [ details [ class "context-help-disclosure" ]
            [ summary
                [ attribute "aria-label" label
                , attribute "aria-describedby" (helpId ++ "-text")
                , attribute "aria-controls" (helpId ++ "-text")
                ]
                [ Ui.Icon.view "help" ]
            ]
        , div
            [ id (helpId ++ "-text")
            , class "context-help-content"
            , attribute "role" "tooltip"
            ]
            [ text explanation ]
        ]
