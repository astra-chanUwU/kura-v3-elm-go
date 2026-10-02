module Feature.Selection exposing (controls)

import Html exposing (Html, button, div, span, text)
import Html.Attributes exposing (class, type_)
import Html.Events exposing (onClick)
import Set exposing (Set)


controls : Set String -> (() -> msg) -> Html msg
controls selected clear =
    if Set.isEmpty selected then
        div [ class "selection-bar selection-bar-empty" ] []

    else
        div [ class "selection-bar" ]
            [ span [ class "selection-count" ] [ text (String.fromInt (Set.size selected) ++ " selected") ]
            , button [ class "button button-subtle", type_ "button", onClick (clear ()) ] [ text "Clear selection" ]
            ]
