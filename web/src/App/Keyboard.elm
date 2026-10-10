module App.Keyboard exposing
    ( KeyEvent
    , Modifiers
    , Target(..)
    , bodyDecoder
    , decoder
    , modifiersDecoder
    )

import Json.Decode as Decode exposing (Decoder)


type Target
    = Editable
    | Control
    | Other


type alias KeyEvent =
    { key : String
    , ctrl : Bool
    , shift : Bool
    , alt : Bool
    , target : Target
    }


type alias Modifiers =
    { ctrl : Bool
    , shift : Bool
    }


decoder : Decoder KeyEvent
decoder =
    Decode.map5 KeyEvent
        (Decode.field "key" Decode.string)
        ctrlDecoder
        (boolField "shiftKey")
        (boolField "altKey")
        targetDecoder


{-| Only key events whose target is `<body>`, i.e. nothing inside the app has
focus. Events from inside the app are handled by the shell's own listener,
which can prevent browser defaults.
-}
bodyDecoder : Decoder KeyEvent
bodyDecoder =
    tagName
        |> Decode.andThen
            (\tag ->
                if tag == "BODY" || tag == "HTML" then
                    decoder

                else
                    Decode.fail "handled by the shell"
            )


modifiersDecoder : Decoder Modifiers
modifiersDecoder =
    Decode.map2 Modifiers ctrlDecoder (boolField "shiftKey")


ctrlDecoder : Decoder Bool
ctrlDecoder =
    Decode.map2 (||) (boolField "ctrlKey") (boolField "metaKey")


targetDecoder : Decoder Target
targetDecoder =
    Decode.map2
        (\tag editable ->
            if editable || List.member tag [ "INPUT", "TEXTAREA", "SELECT" ] then
                Editable

            else if List.member tag [ "BUTTON", "A", "SUMMARY" ] then
                Control

            else
                Other
        )
        tagName
        (Decode.oneOf [ Decode.at [ "target", "isContentEditable" ] Decode.bool, Decode.succeed False ])


tagName : Decoder String
tagName =
    Decode.oneOf [ Decode.at [ "target", "tagName" ] Decode.string, Decode.succeed "" ]


boolField : String -> Decoder Bool
boolField name =
    Decode.oneOf [ Decode.field name Decode.bool, Decode.succeed False ]
