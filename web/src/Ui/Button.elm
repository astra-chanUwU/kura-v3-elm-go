module Ui.Button exposing (Options, icon, view)

import Html exposing (Attribute, Html, button, span, text)
import Html.Attributes exposing (attribute, class, disabled, title, type_)
import Html.Events exposing (onClick)
import Ui.Icon
import Ui.Kbd


type alias Options msg =
    { label : String
    , key : Maybe String
    , onPress : Maybe msg
    , pressed : Maybe Bool
    , hint : Maybe String
    }


{-| A button with an optional shortcut hint. `onPress = Nothing` renders it
disabled; `hint` explains why or what it does.
-}
view : List (Attribute msg) -> Options msg -> Html msg
view attributes options =
    button (buttonAttributes attributes options)
        (span [] [ text options.label ]
            :: (case options.key of
                    Just key ->
                        [ Ui.Kbd.view key ]

                    Nothing ->
                        []
               )
        )


icon : String -> List (Attribute msg) -> Options msg -> Html msg
icon name attributes options =
    button
        (buttonAttributes (class "icon-button" :: attribute "aria-label" options.label :: attributes) options)
        [ Ui.Icon.view name ]


buttonAttributes : List (Attribute msg) -> Options msg -> List (Attribute msg)
buttonAttributes attributes options =
    [ class "button"
    , type_ "button"
    ]
        ++ (case options.onPress of
                Just msg ->
                    [ onClick msg ]

                Nothing ->
                    [ disabled True ]
           )
        ++ (case options.pressed of
                Just pressed ->
                    [ attribute "aria-pressed"
                        (if pressed then
                            "true"

                         else
                            "false"
                        )
                    ]

                Nothing ->
                    []
           )
        ++ (case options.hint of
                Just hint ->
                    [ title hint ]

                Nothing ->
                    []
           )
        ++ attributes
