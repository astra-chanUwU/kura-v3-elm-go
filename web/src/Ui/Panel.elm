module Ui.Panel exposing (Presentation(..), header, presentationClass, section)

import Html exposing (Html, button, div, h2, h3, text)
import Html.Attributes exposing (attribute, class, type_)
import Html.Events exposing (onClick)


type Presentation
    = Docked
    | Drawer
    | Sheet


presentationClass : String -> Presentation -> String
presentationClass base presentation =
    base
        ++ " "
        ++ base
        ++ (case presentation of
                Docked ->
                    "-docked"

                Drawer ->
                    "-drawer"

                Sheet ->
                    "-sheet"
           )


{-| Panel title with a close button when the panel floats over the stage.
-}
header : String -> Presentation -> msg -> Html msg
header title presentation close =
    div [ class "panel-header" ]
        [ h2 [ class "panel-title" ] [ text title ]
        , if presentation == Docked then
            text ""

          else
            button [ class "button button-quiet", type_ "button", onClick close, attribute "aria-label" ("Close " ++ title) ] [ text "×" ]
        ]


section : String -> List (Html msg) -> Html msg
section title content =
    div [ class "panel-section" ] (h3 [ class "panel-section-title" ] [ text title ] :: content)
