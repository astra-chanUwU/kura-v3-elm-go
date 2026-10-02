port module App.Prefs exposing
    ( CellExtras(..)
    , Prefs
    , decoder
    , default
    , extrasLabel
    , nextExtras
    , save
    , thumbMax
    , thumbMin
    , thumbStep
    )

{-| Workspace preferences, read from `localStorage` through flags and written
back through the `savePrefs` port.
-}

import Json.Decode as Decode exposing (Decoder)
import Json.Encode as Encode


port savePrefs : Encode.Value -> Cmd msg


type CellExtras
    = ExtrasOff
    | ExtrasHover
    | ExtrasAlways


type alias Prefs =
    { thumbSize : Int
    , cellExtras : CellExtras
    , navigatorDocked : Bool
    , inspectorDocked : Bool
    , filterBar : Bool
    , savedSearches : List String
    }


default : Prefs
default =
    { thumbSize = 144
    , cellExtras = ExtrasHover
    , navigatorDocked = True
    , inspectorDocked = True
    , filterBar = True
    , savedSearches = []
    }


thumbMin : Int
thumbMin =
    80


thumbMax : Int
thumbMax =
    320


{-| Keyboard steps move between presets; the slider uses 8px steps.
-}
thumbStep : Int -> Int -> Int
thumbStep direction current =
    let
        presets =
            [ 80, 96, 120, 144, 176, 200, 240, 280, 320 ]

        candidates =
            if direction > 0 then
                List.filter (\size -> size > current) presets

            else
                List.filter (\size -> size < current) presets |> List.reverse
    in
    List.head candidates |> Maybe.withDefault current


nextExtras : CellExtras -> CellExtras
nextExtras extras =
    case extras of
        ExtrasOff ->
            ExtrasHover

        ExtrasHover ->
            ExtrasAlways

        ExtrasAlways ->
            ExtrasOff


extrasLabel : CellExtras -> String
extrasLabel extras =
    case extras of
        ExtrasOff ->
            "off"

        ExtrasHover ->
            "hover"

        ExtrasAlways ->
            "always"


decoder : Decoder Prefs
decoder =
    let
        field name fieldDecoder fallback =
            Decode.oneOf [ Decode.field name fieldDecoder, Decode.succeed fallback ]
    in
    Decode.map6 Prefs
        (field "thumbSize" (Decode.map (clamp thumbMin thumbMax) Decode.int) default.thumbSize)
        (field "cellExtras" extrasDecoder default.cellExtras)
        (field "navigatorDocked" Decode.bool default.navigatorDocked)
        (field "inspectorDocked" Decode.bool default.inspectorDocked)
        (field "filterBar" Decode.bool default.filterBar)
        (field "savedSearches" (Decode.list Decode.string) default.savedSearches)


extrasDecoder : Decoder CellExtras
extrasDecoder =
    Decode.string
        |> Decode.map
            (\value ->
                case value of
                    "off" ->
                        ExtrasOff

                    "always" ->
                        ExtrasAlways

                    _ ->
                        ExtrasHover
            )


save : Prefs -> Cmd msg
save prefs =
    savePrefs
        (Encode.object
            [ ( "thumbSize", Encode.int prefs.thumbSize )
            , ( "cellExtras", Encode.string (extrasLabel prefs.cellExtras) )
            , ( "navigatorDocked", Encode.bool prefs.navigatorDocked )
            , ( "inspectorDocked", Encode.bool prefs.inspectorDocked )
            , ( "filterBar", Encode.bool prefs.filterBar )
            , ( "savedSearches", Encode.list Encode.string prefs.savedSearches )
            ]
        )
