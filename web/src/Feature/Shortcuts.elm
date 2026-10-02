module Feature.Shortcuts exposing (elementId, view)

import Html exposing (Html, div, h2, table, tbody, td, text, th, thead, tr)
import Html.Attributes exposing (attribute, class, id, tabindex)
import Ui.Button
import Ui.Kbd


elementId : String
elementId =
    "shortcuts"


view : msg -> Html msg
view close =
    div [ class "shortcuts-backdrop" ]
        [ div
            [ id elementId
            , class "shortcuts"
            , attribute "role" "dialog"
            , attribute "aria-modal" "true"
            , attribute "aria-label" "Keyboard shortcuts"
            , tabindex -1
            ]
            [ div [ class "panel-header" ]
                [ h2 [ class "panel-title" ] [ text "Keyboard shortcuts" ]
                , Ui.Button.view [] { label = "Close", key = Just "Esc", onPress = Just close, pressed = Nothing, hint = Nothing }
                ]
            , table [ class "shortcuts-table" ]
                [ thead [] [ tr [] [ th [] [ text "Keys" ], th [] [ text "Action" ] ] ]
                , tbody [] (List.map row shortcuts)
                ]
            ]
        ]


row : ( List String, String ) -> Html msg
row ( keys, action ) =
    tr [] [ td [] (List.intersperse (text " ") (List.map Ui.Kbd.view keys)), td [] [ text action ] ]


shortcuts : List ( List String, String )
shortcuts =
    [ ( [ "←", "→", "↑", "↓" ], "Move the active post (Compare: ↑ make select, ↓ swap)" )
    , ( [ "Shift", "arrows" ], "Extend the selection from the anchor" )
    , ( [ "Alt", "←", "→" ], "Step through selected posts only (Loupe, Compare)" )
    , ( [ "Home", "End", "PgUp", "PgDn" ], "Jump through the grid" )
    , ( [ "Space" ], "Toggle Quick Look" )
    , ( [ "E", "Enter" ], "Quick Look (Loupe)" )
    , ( [ "C" ], "Compare" )
    , ( [ "N" ], "Survey 2–6 selected posts" )
    , ( [ "G", "Esc" ], "Back to the grid" )
    , ( [ "S" ], "Toggle the active post in the selection" )
    , ( [ "Ctrl", "A" ], "Select all loaded posts" )
    , ( [ "Ctrl", "D" ], "Clear the selection" )
    , ( [ "−", "=" ], "Smaller / larger thumbnails" )
    , ( [ "J" ], "Cycle thumbnail badges" )
    , ( [ "Z" ], "Fit / actual size" )
    , ( [ "/" ], "Focus the query" )
    , ( [ "\\" ], "Toggle the filter bar" )
    , ( [ "I" ], "Toggle the inspector" )
    , ( [ "Shift", "N" ], "Toggle the navigator" )
    , ( [ "?" ], "This sheet" )
    ]
